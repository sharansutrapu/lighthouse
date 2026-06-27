import requests
import json
import subprocess
import time
import os

env = os.environ.copy()
env["DB_PATH"] = "./lighthouse.db"
server = subprocess.Popen(["go", "run", "."], env=env)
time.sleep(3)

session = requests.Session()
session.headers.update({"Origin": "http://localhost:8000", "Referer": "http://localhost:8000/", "x-lighthouse-client": "web"})

login = None
for i in range(10):
    try:
        login = session.post("http://localhost:8000/api/token", data={"username": "admin", "password": "admin123"})
        break
    except requests.exceptions.ConnectionError:
        time.sleep(2)

if not login or login.status_code != 200:
    print("Login failed")
    server.terminate()
    exit(1)

token = login.json()["access_token"]
headers = {"Authorization": f"Bearer {token}", "x-lighthouse-client": "web"}
res = session.put("http://localhost:8000/api/admin/settings", headers=headers, json={"smtp_host": "test"})
print(res.status_code, res.text)
server.terminate()
