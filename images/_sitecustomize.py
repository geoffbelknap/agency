"""Auto-configure agency structured logging on Python startup.

This file is placed on PYTHONPATH and runs automatically before any user
code. Every Python process in an agency container gets structured JSON
logging without any imports or setup.

Only activates when AGENCY_COMPONENT is set (i.e., inside containers).
Skipped during development and CI test runs.
"""
import os

if os.environ.get("AGENCY_COMPONENT"):
    try:
        from logging_config import setup_logging
        setup_logging(os.environ["AGENCY_COMPONENT"])
    except ImportError:
        pass  # logging_config not on path — not in a container
