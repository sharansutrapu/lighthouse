import subprocess
import json
import time
import sys
import threading

def read_output(proc):
    for line in iter(proc.stdout.readline, b''):
        print(f"[MCP RESPONSE] {line.decode('utf-8').strip()}")
        
def read_err(proc):
    for line in iter(proc.stderr.readline, b''):
        print(f"[MCP STDERR] {line.decode('utf-8').strip()}")

cmd = [
    "npx", "-y", "@cloudmcp/connect",
    "--url", "https://lighthouse.sirgiving.org/api/mcp/sse",
    "--header", "Authorization: Bearer lh_pat_439833bfe6c7059ea311341944509896023c807f5255c8f0e7c6026d50cbb621"
]

print("Starting MCP transport bridge...")
proc = subprocess.Popen(
    cmd,
    stdin=subprocess.PIPE,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE
)

threading.Thread(target=read_output, args=(proc,), daemon=True).start()
threading.Thread(target=read_err, args=(proc,), daemon=True).start()

print("Waiting for bridge to connect...")
time.sleep(3)

print("Sending initialize request...")
init_req = {
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
        "protocolVersion": "2024-11-05",
        "capabilities": {},
        "clientInfo": {"name": "test-client", "version": "1.0.0"}
    }
}
proc.stdin.write((json.dumps(init_req) + "\n").encode('utf-8'))
proc.stdin.flush()

time.sleep(2)

print("Sending tools/list request...")
tools_req = {
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/list",
    "params": {}
}
proc.stdin.write((json.dumps(tools_req) + "\n").encode('utf-8'))
proc.stdin.flush()

time.sleep(3)

print("Terminating...")
proc.terminate()
