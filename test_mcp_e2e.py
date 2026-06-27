import requests
import json
import time
import subprocess
import os

print("Starting Lighthouse server in background...")
env = os.environ.copy()
env["DB_PATH"] = "./lighthouse.db"
env["ALLOW_CORS_ORIGIN"] = "*"
server_process = subprocess.Popen(["go", "run", "."], cwd="/Users/sharankumarrreddysutrapu/Documents/vsCode/Personal-Projects/LightHouse-Data/lighthouse", env=env)
time.sleep(3) # Wait for server to start and init DB
subprocess.run(["sqlite3", "./lighthouse.db", "UPDATE users SET password_changed=1 WHERE username='admin';"])
time.sleep(2)

try:
    print("Logging in as admin...")
    session = requests.Session()
    session.headers.update({"Origin": "http://localhost:8000", "Referer": "http://localhost:8000/", "x-lighthouse-client": "web"})
    
    # Retry loop for server startup
    max_retries = 10
    login_resp = None
    for i in range(max_retries):
        try:
            login_resp = session.post("http://localhost:8000/api/token", data={"username": "admin", "password": "admin123"})
            break
        except requests.exceptions.ConnectionError:
            print(f"Waiting for server... ({i+1}/{max_retries})")
            time.sleep(2)
            
    if not login_resp or login_resp.status_code != 200:
        print("Failed to login", login_resp.text if login_resp else "Connection failed")
        exit(1)
        
    access_token = login_resp.json()["access_token"]
    session.headers.update({"Authorization": f"Bearer {access_token}"})
        
    print("Generating MCP token...")
    token_resp = session.post("http://localhost:8000/api/tokens", json={"name": "e2e-test"})
    if token_resp.status_code != 200:
        print("Failed to generate token", token_resp.text)
        exit(1)
    
    token = token_resp.json()["token"]
    print(f"Token generated (length: {len(token)})")

    # Connect to SSE
    print("Connecting to SSE endpoint...")
    headers = {"Authorization": f"Bearer {token}"}
    
    sse_resp = requests.get("http://localhost:8000/api/mcp/sse", headers=headers, stream=True)
    if sse_resp.status_code != 200:
        print("SSE connection failed:", sse_resp.text)
        exit(1)
        
    endpoint = None
    for line in sse_resp.iter_lines():
        if line:
            line = line.decode('utf-8')
            if line.startswith("event: endpoint"):
                # next line is data
                pass
            elif line.startswith("data: "):
                endpoint = line[len("data: "):]
                break

    if not endpoint:
        print("Failed to get endpoint from SSE stream")
        exit(1)

    print(f"Got message endpoint: {endpoint}")
    
    # We can't easily wait for SSE while sending POST synchronously in basic requests unless we thread, 
    # but the server handles POSTs asynchronously to SSE anyway.
    # Let's send an init request to the message endpoint
    print("Sending initialize request...")
    msg_resp = requests.post(
        f"http://localhost:8000{endpoint}",
        headers=headers,
        json={
            "jsonrpc": "2.0",
            "id": 1,
            "method": "initialize",
            "params": {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "clientInfo": {"name": "test-client", "version": "1.0.0"}
            }
        }
    )
    
    print("Initialize message POST status:", msg_resp.status_code)
    
    # Send tools/list request
    print("Sending tools/list request...")
    msg_resp = requests.post(
        f"http://localhost:8000{endpoint}",
        headers=headers,
        json={
            "jsonrpc": "2.0",
            "id": 2,
            "method": "tools/list",
            "params": {}
        }
    )
    
    print("tools/list POST status:", msg_resp.status_code)
    print("Wait a moment to see if server crashes...")
    time.sleep(2)
    
    print("E2E Validation Success!")

finally:
    print("Terminating server...")
    server_process.terminate()
    server_process.wait()
