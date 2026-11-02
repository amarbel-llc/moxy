"""A stand-in policy broker for the bats lane (RFC 0002 wire: NDJSON, v1).

Usage: stand_in_broker.py LISTEN MODE KEY LOGFILE READYFILE [ALLOWED]

LISTEN  an absolute unix socket path, or "tcp" for 127.0.0.1 on a free port
MODE    allow | ask | deny   answer every check with that verdict
        policy               a stand-in for a spinclass folio stanza: allow a
                             file under one of the comma-separated ALLOWED
                             directory prefixes (never under /.spinclass/ other
                             than spawn.log), allow a blob only when ALLOWED
                             lists "blob", deny everything else
        hang                 hello, then never answer a check
        nohello              accept, never say hello
        drop                 hello, then close the connection on the first check
        unlistening          bind the socket path but never listen (ECONNREFUSED)
KEY     the session_key in the broker's hello
LOGFILE one JSON line per client message received
READYFILE written (with the address clients dial) once the broker is up
"""

import json
import os
import socket
import sys
import threading
import time

listen, mode, key, logfile, readyfile = sys.argv[1:6]
allowed = sys.argv[6].split(",") if len(sys.argv) > 6 else []
loglock = threading.Lock()


def policy_verdict(resource):
    uri = resource.get("uri", "")
    if resource.get("kind") == "blob":
        return "allow" if "blob" in allowed else "deny"
    path = uri.removeprefix("file://")
    if "/.spinclass/" in path and not path.endswith("/spawn.log"):
        return "deny"
    for prefix in allowed:
        if prefix and prefix != "blob" and (path == prefix or path.startswith(prefix + "/")):
            return "allow"
    return "deny"


def log(m):
    with loglock, open(logfile, "a") as f:
        f.write(json.dumps(m) + "\n")


def send(conn, m):
    conn.sendall((json.dumps(m) + "\n").encode())


def serve(conn):
    r = conn.makefile("rb")
    first = r.readline()
    if not first:
        return
    log(json.loads(first))
    if mode == "nohello":
        r.readline()
        return
    send(conn, {"v": 1, "type": "hello", "broker": "stand-in",
                "capabilities": ["check"], "session_key": key})
    for line in r:
        m = json.loads(line)
        log(m)
        if m.get("type") != "check":
            continue
        if mode == "drop":
            conn.close()
            return
        if mode == "hang":
            continue
        verdict = policy_verdict(m.get("resource") or {}) if mode == "policy" else mode
        send(conn, {"v": 1, "type": "verdict", "id": m["id"],
                    "verdict": verdict, "reason": "stand-in " + verdict})


if mode == "unlistening":
    s = socket.socket(socket.AF_UNIX)
    s.bind(listen)
    addr = listen
else:
    if listen == "tcp":
        s = socket.socket()
        s.bind(("127.0.0.1", 0))
        addr = "127.0.0.1:%d" % s.getsockname()[1]
    else:
        s = socket.socket(socket.AF_UNIX)
        s.bind(listen)
        addr = listen
    s.listen(8)

tmp = readyfile + ".tmp"
with open(tmp, "w") as f:
    f.write(addr)
os.rename(tmp, readyfile)

if mode == "unlistening":
    time.sleep(3600)

while True:
    c, _ = s.accept()
    threading.Thread(target=serve, args=(c,), daemon=True).start()
