"""Auto-configure agency structured logging on Python startup.

This file is placed on PYTHONPATH and runs automatically before any user
code. Every Python process in an agency container gets structured JSON
logging without any imports or setup.
"""
import os

from logging_config import setup_logging

setup_logging(os.environ.get("AGENCY_COMPONENT", "unknown"))
