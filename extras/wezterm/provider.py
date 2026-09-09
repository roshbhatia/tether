DEFAULT_CORE = 'tether'
TITLE = 'Hosts'
ICON = 'md_server'

import json
import os
import subprocess
import shutil
import sys
from pathlib import Path

CORE = sys.argv[1] if len(sys.argv) > 1 else DEFAULT_CORE

def run(*args):
    result = subprocess.run([CORE, *args], text=True, capture_output=True, timeout=4, check=True)
    return json.loads(result.stdout)

def segment(text, role):
    return {"text": str(text), "role": role}

def hosts():
    return run("hosts", "--json")["hosts"]

def items():
    result = []
    for host in hosts():
        peer = host.get("peer") or {}
        state = ("online" if peer.get("online") else "offline") if peer else "unknown"
        result.append({"id": host["name"], "search": host["name"] + " " + host.get("hostname", ""),
                       "segments": [segment(host["name"], "name"), segment(host.get("hostname", ""), "path"), segment(state, "detail")]})
    return result

def resolve(item):
    host = next((row for row in hosts() if row["name"] == item), None)
    if host is None:
        raise ValueError("host no longer exists")
    executable = str(Path(CORE).with_name("tsh")) if "/" in CORE else "tsh"
    return {"kind": "spawn", "label": item, "cwd": str(Path.home()),
            "command": [executable, host.get("target") or item], "environment": {}}

def main():
    request = json.load(sys.stdin)
    frame = {"version": "provider/v1", "kind": "result", "requestId": request.get("requestId", "invalid")}
    try:
        if request.get("version") != "provider/v1" or request.get("kind") != "request":
            raise ValueError("expected a provider/v1 request")
        capability = request["capability"]
        if capability == "provider.validate":
            if not shutil.which(CORE):
                raise FileNotFoundError("core executable is unavailable: " + CORE)
            subprocess.run([CORE, "--help"], text=True, capture_output=True, timeout=4, check=True)
            connector = str(Path(CORE).with_name("tsh")) if "/" in CORE else "tsh"
            if not shutil.which(connector):
                raise FileNotFoundError("connection executable is unavailable: " + connector)
            output = {"ok": True}
        elif capability == "picker.describe":
            output = {"title": TITLE, "icon": ICON}
        elif capability == "picker.list":
            output = {"items": items()}
        elif capability == "picker.open":
            output = resolve(request["input"]["id"])
        else:
            raise ValueError("unsupported capability: " + capability)
        frame.update(status="ok", output=output)
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        frame.update(status="error", message=str(error))
    print(json.dumps(frame))

if __name__ == "__main__":
    main()
