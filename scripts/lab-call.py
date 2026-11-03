"""Send one request to an inua shell's control socket, and print its answer.

    lab-call.py SOCKET TARGET FUNCTION [ARGS…]

What `bin/inua call` does, for a socket given by path: used to drive a
shell in the test lab, which has its own instance name.
"""
import json
import socket
import sys

path, target, function, *args = sys.argv[1:]
if not (path and target and function):
    sys.exit("usage: lab-call.py SOCKET TARGET FUNCTION [ARGS…]")
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.connect(path)
s.sendall((json.dumps({"op": "call", "target": target, "function": function, "args": args}) + "\n").encode())
reply = json.loads(s.makefile().readline())
print(reply.get("result") or reply.get("error") or "")
sys.exit(0 if reply.get("ok") else 1)
