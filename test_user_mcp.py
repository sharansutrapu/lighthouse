import requests
import json
import time
import sys

URL = "https://lighthouse.sirgiving.org/api/mcp/sse"
TOKEN = "lh_pat_61aa283259f62286f1987997ed859d5abbf1061bd5769dc74f67c04e5fa77e97"

headers = {
    "Authorization": f"Bearer {TOKEN}",
    "Accept": "text/event-stream"
}

print(f"[*] Connecting to {URL}...")
res = requests.get(URL, headers=headers, stream=True)

if res.status_code != 200:
    print(f"[-] FAILED to connect: {res.status_code} {res.text}")
    sys.exit(1)

print("[+] Connected! Waiting for endpoint event...")
post_url = None

for line in res.iter_lines(decode_unicode=True):
    if line:
        print(f"RAW: {line}")
        if line.startswith("event: endpoint"):
            pass
        elif line.startswith("data: "):
            data = line[6:]
            if data.startswith("http"):
                post_url = data
            elif data.startswith("/"):
                # Handle relative path
                from urllib.parse import urlparse
                parsed = urlparse(URL)
                post_url = f"{parsed.scheme}://{parsed.netloc}{data}"
            else:
                post_url = f"{URL}/{data}"
                
            if post_url:
                print(f"[+] Received POST endpoint: {post_url}")
                break

if not post_url:
    print("[-] Did not receive endpoint URL")
    sys.exit(1)

print("[*] Sending initialize request...")
init_payload = {
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
        "protocolVersion": "2024-11-05",
        "capabilities": {},
        "clientInfo": {"name": "test-client", "version": "1.0.0"}
    }
}
post_res = requests.post(post_url, headers={"Authorization": f"Bearer {TOKEN}", "Content-Type": "application/json"}, json=init_payload)
print(f"POST initialize status: {post_res.status_code}")
if post_res.status_code != 200:
    print(f"Error response: {post_res.text}")

print("[*] Waiting for initialize response...")
for line in res.iter_lines(decode_unicode=True):
    if line and line.startswith("data: "):
        try:
            data = json.loads(line[6:])
            if data.get("id") == 1:
                print("[+] Initialized successfully.")
                break
        except Exception:
            pass

print("[*] Sending notifications/initialized...")
requests.post(post_url, headers={"Authorization": f"Bearer {TOKEN}", "Content-Type": "application/json"}, json={"jsonrpc": "2.0", "method": "notifications/initialized", "params": {}})

print("[*] Sending tools/list request...")
tools_payload = {
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/list",
    "params": {}
}
requests.post(post_url, headers={"Authorization": f"Bearer {TOKEN}", "Content-Type": "application/json"}, json=tools_payload)

print("[*] Waiting for tools list...")
for line in res.iter_lines(decode_unicode=True):
    if line and line.startswith("data: "):
        try:
            data = json.loads(line[6:])
            if data.get("id") == 2:
                print("\n[+] Received tools list:")
                print(json.dumps(data, indent=2))
                break
        except Exception:
            pass

print("[+] Validation Complete!")
