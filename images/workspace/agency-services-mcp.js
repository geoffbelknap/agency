#!/usr/bin/env node
// Agency Services MCP Server
//
// Reads /agency/services-manifest.json and exposes granted services as MCP
// tools. Routes all HTTP through the enforcer with X-Agency-Service headers.
// No real API keys enter the workspace.
//
// Protocol: MCP stdio (JSON-RPC over stdin/stdout)
// Dependencies: Node.js builtins only (fs, http, https, url)

'use strict';

const fs = require('fs');
const http = require('http');
const https = require('https');
const { URL } = require('url');

const MANIFEST_PATH = '/agency/services-manifest.json';
const ENFORCER_HOST = 'enforcer';
const ENFORCER_PORT = 18080;

let manifest = null;

function log(msg) {
  process.stderr.write(`[agency-services-mcp] ${msg}\n`);
}

function loadManifest() {
  try {
    manifest = JSON.parse(fs.readFileSync(MANIFEST_PATH, 'utf8'));
    log(`Loaded manifest: ${manifest.services.length} service(s)`);
  } catch (err) {
    log(`Failed to load manifest: ${err.message}`);
    manifest = { version: 1, agent: 'unknown', services: [] };
  }
}

function buildToolList() {
  const tools = [];
  for (const svc of manifest.services) {
    for (const tool of svc.tools) {
      const properties = {};
      const required = [];
      for (const param of tool.parameters || []) {
        properties[param.name] = {
          type: param.type || 'string',
          description: param.description,
        };
        if (param.default !== undefined && param.default !== null) {
          properties[param.name].default = param.default;
        }
        if (param.required) {
          required.push(param.name);
        }
      }
      tools.push({
        name: tool.name,
        description: tool.description,
        inputSchema: {
          type: 'object',
          properties,
          required,
        },
      });
    }
  }
  return tools;
}

function findToolAndService(toolName) {
  for (const svc of manifest.services) {
    for (const tool of svc.tools) {
      if (tool.name === toolName) {
        return { service: svc, tool };
      }
    }
  }
  return null;
}

function substitutePath(pathTemplate, args) {
  return pathTemplate.replace(/\{(\w+)\}/g, (_, key) => {
    const val = args[key];
    if (val === undefined) return `{${key}}`;
    return encodeURIComponent(String(val));
  });
}

function extractResponsePath(data, dotPath) {
  if (!dotPath) return data;
  const parts = dotPath.split('.');
  let current = data;
  for (const part of parts) {
    if (current == null || typeof current !== 'object') return current;
    current = current[part];
  }
  return current;
}

function callTool(svc, tool, args) {
  return new Promise((resolve, reject) => {
    const path = substitutePath(tool.path, args);
    const url = new URL(path, svc.api_base);

    // Map tool args to query parameters via query_params config
    if (tool.query_params) {
      for (const [argName, queryKey] of Object.entries(tool.query_params)) {
        const val = args[argName];
        if (val !== undefined && val !== null) {
          url.searchParams.set(queryKey, String(val));
        }
      }
    }

    // Build body for POST/PUT
    let body = null;
    if (tool.body_template && (tool.method === 'POST' || tool.method === 'PUT')) {
      const bodyStr = JSON.stringify(tool.body_template);
      body = bodyStr.replace(/\{(\w+)\}/g, (_, key) => {
        const val = args[key];
        return val !== undefined ? String(val) : `{${key}}`;
      });
    }

    const headers = {
      'X-Agency-Service': svc.service,
      'Authorization': `Bearer ${svc.scoped_token}`,
      'Accept': 'application/json',
      'Host': url.host,
    };
    if (body) {
      headers['Content-Type'] = 'application/json';
      headers['Content-Length'] = Buffer.byteLength(body);
    }

    // Use absolute-form URI (HTTP proxy protocol): the enforcer extracts
    // the full URL from the request line and forwards through egress.
    const options = {
      hostname: ENFORCER_HOST,
      port: ENFORCER_PORT,
      path: url.toString(),
      method: tool.method || 'GET',
      headers,
    };

    log(`${options.method} ${svc.api_base}${url.pathname}${url.search}`);

    const req = http.request(options, (res) => {
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => {
        const raw = Buffer.concat(chunks).toString('utf8');
        if (res.statusCode >= 400) {
          resolve({
            isError: true,
            content: [{ type: 'text', text: `HTTP ${res.statusCode}: ${raw}` }],
          });
          return;
        }
        try {
          let data = JSON.parse(raw);
          data = extractResponsePath(data, tool.response_path);
          resolve({
            content: [{ type: 'text', text: JSON.stringify(data, null, 2) }],
          });
        } catch {
          resolve({
            content: [{ type: 'text', text: raw }],
          });
        }
      });
    });

    req.on('error', (err) => {
      resolve({
        isError: true,
        content: [{ type: 'text', text: `Request failed: ${err.message}` }],
      });
    });

    if (body) req.write(body);
    req.end();
  });
}

// -- JSON-RPC / MCP protocol --

let inputBuffer = '';

function sendResponse(id, result) {
  const msg = JSON.stringify({ jsonrpc: '2.0', id, result }) + '\n';
  process.stdout.write(msg);
}

function sendError(id, code, message) {
  const msg = JSON.stringify({
    jsonrpc: '2.0',
    id,
    error: { code, message },
  }) + '\n';
  process.stdout.write(msg);
}

async function handleMessage(msg) {
  const { id, method, params } = msg;

  switch (method) {
    case 'initialize':
      sendResponse(id, {
        protocolVersion: '2024-11-05',
        capabilities: { tools: {} },
        serverInfo: { name: 'agency-services', version: '1.0.0' },
      });
      break;

    case 'notifications/initialized':
      // No response needed for notifications
      break;

    case 'tools/list':
      sendResponse(id, { tools: buildToolList() });
      break;

    case 'tools/call': {
      const toolName = params && params.name;
      const args = (params && params.arguments) || {};
      const found = findToolAndService(toolName);
      if (!found) {
        sendResponse(id, {
          isError: true,
          content: [{ type: 'text', text: `Unknown tool: ${toolName}` }],
        });
        break;
      }
      try {
        const result = await callTool(found.service, found.tool, args);
        sendResponse(id, result);
      } catch (err) {
        sendResponse(id, {
          isError: true,
          content: [{ type: 'text', text: `Tool error: ${err.message}` }],
        });
      }
      break;
    }

    default:
      if (id !== undefined) {
        sendError(id, -32601, `Method not found: ${method}`);
      }
  }
}

// -- Direct tool call mode --
// When invoked with --call <tool_name> <args...>, calls a single tool
// and prints the result to stdout. Used by CLI wrappers.

async function directCall() {
  loadManifest();
  const toolName = process.argv[3];
  const found = findToolAndService(toolName);
  if (!found) {
    process.stderr.write(`Unknown tool: ${toolName}\n`);
    process.exit(1);
  }

  // Build args from remaining positional arguments matched to tool parameters
  const args = {};
  const toolParams = found.tool.parameters || [];
  const cliArgs = process.argv.slice(4);
  for (let i = 0; i < toolParams.length && i < cliArgs.length; i++) {
    if (cliArgs[i]) {
      args[toolParams[i].name] = cliArgs[i];
    }
  }

  const result = await callTool(found.service, found.tool, args);
  if (result.isError) {
    process.stderr.write(result.content[0].text + '\n');
    process.exit(1);
  }
  process.stdout.write(result.content[0].text + '\n');
  process.exit(0);
}

// -- Entry point --

if (process.argv[2] === '--call') {
  directCall();
} else {
  // Default: MCP stdio server mode
  loadManifest();
  log('MCP server started');

  process.stdin.setEncoding('utf8');
  process.stdin.on('data', (chunk) => {
    inputBuffer += chunk;
    let newlineIdx;
    while ((newlineIdx = inputBuffer.indexOf('\n')) !== -1) {
      const line = inputBuffer.slice(0, newlineIdx).trim();
      inputBuffer = inputBuffer.slice(newlineIdx + 1);
      if (!line) continue;
      try {
        const msg = JSON.parse(line);
        handleMessage(msg);
      } catch (err) {
        log(`Parse error: ${err.message}`);
      }
    }
  });

  process.stdin.on('end', () => {
    log('stdin closed, exiting');
    process.exit(0);
  });
}
