#!/usr/bin/env python3
"""
Lightweight deploy webhook for AI Battle Arena.
Listens for POST requests from GitHub Actions to trigger deploys.

Usage: python3 deploy-webhook.py
Runs on port 8702, expects X-Deploy-Token header.
"""

import http.server
import json
import os
import subprocess
import threading
import sys

PORT = 8702
DEPLOY_TOKEN = os.environ.get("ARENA_DEPLOY_TOKEN", "")
ARENA_DIR = "/opt/ai-battle-arena"

DEPLOY_TARGETS = {
    "beta": {
        "branch": "develop",
        "compose_cmd": [
            "docker", "compose",
            "-f", "docker-compose.yml",
            "-f", "docker-compose.beta.yml",
            "up", "-d", "--build", "arena-server-beta"
        ],
    },
    "production": {
        "branch": "main",
        "compose_cmd": [
            "docker", "compose",
            "up", "-d", "--build", "arena-server"
        ],
    },
}


def run_deploy(target_name, target_cfg):
    """Run deploy in background thread."""
    branch = target_cfg["branch"]
    print(f"[DEPLOY] Starting {target_name} deploy from {branch}...")

    try:
        # Pull latest
        subprocess.run(
            ["git", "fetch", "origin", branch],
            cwd=ARENA_DIR, check=True, capture_output=True, text=True
        )
        subprocess.run(
            ["git", "checkout", branch],
            cwd=ARENA_DIR, check=True, capture_output=True, text=True
        )
        subprocess.run(
            ["git", "pull", "origin", branch],
            cwd=ARENA_DIR, check=True, capture_output=True, text=True
        )

        # Build and deploy
        result = subprocess.run(
            target_cfg["compose_cmd"],
            cwd=ARENA_DIR, capture_output=True, text=True, timeout=300
        )
        if result.returncode == 0:
            print(f"[DEPLOY] ✅ {target_name} deployed successfully")
        else:
            print(f"[DEPLOY] ❌ {target_name} deploy failed: {result.stderr}")

    except Exception as e:
        print(f"[DEPLOY] ❌ {target_name} deploy error: {e}")


class DeployHandler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        # Auth check
        token = self.headers.get("X-Deploy-Token", "")
        if not DEPLOY_TOKEN or token != DEPLOY_TOKEN:
            self.send_response(403)
            self.end_headers()
            self.wfile.write(b'{"error": "forbidden"}')
            return

        # Parse body
        content_len = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(content_len)) if content_len > 0 else {}

        target = body.get("target", "")
        if target not in DEPLOY_TARGETS:
            self.send_response(400)
            self.end_headers()
            self.wfile.write(json.dumps({"error": f"unknown target: {target}"}).encode())
            return

        # Kick off deploy in background
        cfg = DEPLOY_TARGETS[target]
        thread = threading.Thread(target=run_deploy, args=(target, cfg), daemon=True)
        thread.start()

        self.send_response(200)
        self.end_headers()
        self.wfile.write(json.dumps({"status": "deploying", "target": target}).encode())

    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'{"status": "ok", "service": "arena-deploy-webhook"}')

    def log_message(self, format, *args):
        print(f"[WEBHOOK] {args[0]}")


if __name__ == "__main__":
    if not DEPLOY_TOKEN:
        print("ERROR: ARENA_DEPLOY_TOKEN not set")
        sys.exit(1)

    server = http.server.HTTPServer(("127.0.0.1", PORT), DeployHandler)
    print(f"[WEBHOOK] Deploy webhook listening on 127.0.0.1:{PORT}")
    server.serve_forever()
