#!/usr/bin/env python3
"""Fetch external DNS blocklists for the Agency egress proxy.

Runs at container startup before mitmproxy. Downloads community-maintained
blocklists, parses hosts-file or plain domain-list formats, and writes
parsed domains to /app/blocklists/{name}.txt.

Graceful on failure: uses cached files if available, otherwise skips source.
"""

import json
import sys
import time
from pathlib import Path
from urllib.request import Request, urlopen
from urllib.error import URLError

import yaml


def parse_hosts_file(content):
    """Parse hosts-file format (0.0.0.0 domain or 127.0.0.1 domain)."""
    domains = set()
    skip_hosts = {"localhost", "localhost.localdomain", "local", "broadcasthost",
                  "ip6-localhost", "ip6-loopback", "ip6-localnet",
                  "ip6-mcastprefix", "ip6-allnodes", "ip6-allrouters",
                  "ip6-allhosts"}
    for line in content.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split()
        if len(parts) < 2:
            continue
        # First field is the IP (0.0.0.0 or 127.0.0.1), second is domain
        ip_part = parts[0]
        if not (ip_part.startswith("0.0.0.0") or ip_part.startswith("127.0.0.1")
                or ip_part == "::1"):
            continue
        domain = parts[1].lower().strip()
        # Strip inline comments
        if "#" in domain:
            domain = domain.split("#")[0].strip()
        if not domain or domain in skip_hosts:
            continue
        if domain.startswith("ip6-"):
            continue
        domains.add(domain)
    return domains


def parse_domain_list(content):
    """Parse plain domain-list format (one domain per line)."""
    domains = set()
    for line in content.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        domain = line.lower().split()[0]
        if domain and "." in domain:
            domains.add(domain)
    return domains


def fetch_source(source, blocklist_dir, metadata, timeout):
    """Fetch a single blocklist source. Returns (name, domain_count, status)."""
    name = source["name"]
    url = source["url"]
    fmt = source.get("format", "hosts")
    cache_file = blocklist_dir / f"{name}.txt"

    headers = {}
    cached_meta = metadata.get(name, {})
    if cached_meta.get("etag"):
        headers["If-None-Match"] = cached_meta["etag"]
    if cached_meta.get("last_modified"):
        headers["If-Modified-Since"] = cached_meta["last_modified"]

    try:
        req = Request(url, headers=headers)
        resp = urlopen(req, timeout=timeout)

        if resp.status == 304:
            # Not modified, use cached
            if cache_file.exists():
                count = sum(1 for line in cache_file.read_text().splitlines() if line.strip())
                return name, count, "not_modified"
            # Fall through to re-download without conditional headers
            req = Request(url)
            resp = urlopen(req, timeout=timeout)

        content = resp.read().decode("utf-8", errors="replace")

        # Parse based on format
        if fmt == "hosts":
            domains = parse_hosts_file(content)
        else:
            domains = parse_domain_list(content)

        # Write parsed domains
        cache_file.write_text("\n".join(sorted(domains)) + "\n")

        # Update metadata
        new_meta = {}
        etag = resp.headers.get("ETag")
        if etag:
            new_meta["etag"] = etag
        last_mod = resp.headers.get("Last-Modified")
        if last_mod:
            new_meta["last_modified"] = last_mod
        new_meta["fetched_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        metadata[name] = new_meta

        return name, len(domains), "fetched"

    except URLError as e:
        # Use cached file if available
        if cache_file.exists():
            count = sum(1 for line in cache_file.read_text().splitlines() if line.strip())
            return name, count, f"fetch_failed_using_cache ({e.reason})"
        return name, 0, f"fetch_failed ({e.reason})"
    except Exception as e:
        if cache_file.exists():
            count = sum(1 for line in cache_file.read_text().splitlines() if line.strip())
            return name, count, f"error_using_cache ({e})"
        return name, 0, f"error ({e})"


def main(policy_path, blocklist_dir):
    blocklist_dir = Path(blocklist_dir)
    blocklist_dir.mkdir(parents=True, exist_ok=True)

    # Load policy
    try:
        with open(policy_path) as f:
            policy = yaml.safe_load(f) or {}
    except FileNotFoundError:
        print(f"[fetch_blocklists] Policy not found: {policy_path}", file=sys.stderr)
        return
    except Exception as e:
        print(f"[fetch_blocklists] Error loading policy: {e}", file=sys.stderr)
        return

    bl_config = policy.get("external_blocklists", {})
    if not bl_config or not bl_config.get("enabled", False):
        print("[fetch_blocklists] External blocklists disabled or not configured", file=sys.stderr)
        return

    sources = bl_config.get("sources", [])
    if not sources:
        print("[fetch_blocklists] No blocklist sources configured", file=sys.stderr)
        return

    timeout = bl_config.get("fetch_timeout_seconds", 30)

    # Load cached metadata
    metadata_file = blocklist_dir / "metadata.json"
    metadata = {}
    if metadata_file.exists():
        try:
            metadata = json.loads(metadata_file.read_text())
        except Exception:
            metadata = {}

    # Fetch each source
    fetch_log = []
    for source in sources:
        name, count, status = fetch_source(source, blocklist_dir, metadata, timeout)
        entry = {"name": name, "domains": count, "status": status}
        fetch_log.append(entry)
        print(f"[fetch_blocklists] {name}: {count} domains ({status})", file=sys.stderr)

    # Save metadata
    metadata_file.write_text(json.dumps(metadata, indent=2) + "\n")

    # Write fetch log
    log_file = blocklist_dir / "fetch-log.json"
    log_file.write_text(json.dumps({
        "fetched_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "sources": fetch_log,
    }, indent=2) + "\n")


if __name__ == "__main__":
    if len(sys.argv) < 3:
        print(f"Usage: {sys.argv[0]} <policy.yaml> <blocklist_dir>", file=sys.stderr)
        sys.exit(1)
    main(sys.argv[1], sys.argv[2])
