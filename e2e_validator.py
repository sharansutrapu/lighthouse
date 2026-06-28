import requests
import time
import subprocess
import os
import sys

PORT = 8081
BASE_URL = f"http://localhost:{PORT}/api"
TEST_DB = "test_e2e.db"

print("=============================================")
print("Lighthouse End-to-End Validation Suite")
print("=============================================")

print(f"[*] Starting local backend on port {PORT} with isolated DB ({TEST_DB})...")
if os.path.exists(TEST_DB):
    os.remove(TEST_DB)

env = os.environ.copy()
env["PORT"] = str(PORT)
env["DB_PATH"] = TEST_DB
env["SECRET_KEY"] = "e2e_test_secret"
env["CLIENT_ACCESS"] = "off"

server_log = open("server.log", "w")
print("[*] Building backend binary...")
subprocess.run(["go", "build", "-o", "e2e_backend", "."], check=True)
SERVER_PROC = subprocess.Popen(
    ["./e2e_backend"],
    env=env,
    stdout=server_log,
    stderr=subprocess.STDOUT
)

for i in range(15):
    try:
        res = requests.get(f"http://localhost:{PORT}/")
        if res.status_code:
            print("[+] Server is up!")
            break
    except:
        time.sleep(1)
else:
    print("[-] FAILED to start server. Check server.log:")
    with open("server.log", "r") as f:
        print(f.read())
    SERVER_PROC.terminate()
    sys.exit(1)

def get_token(username, password):
    headers = {
        "X-Lighthouse-Client": "web",
        "Origin": f"http://localhost:{PORT}"
    }
    res = requests.post(f"{BASE_URL}/token", data={"username": username, "password": password}, headers=headers)
    print(f"DEBUG status={res.status_code} json={res.text}")
    if res.status_code == 200:
        return res.json().get("access_token")
    return None

admin_token = get_token("admin", "admin123")
if not admin_token:
    print("[-] FAILED: Could not log in as admin")
    SERVER_PROC.terminate()
    sys.exit(1)

auth_headers = {
    "Authorization": f"Bearer {admin_token}",
    "Content-Type": "application/json",
    "X-Lighthouse-Client": "web",
    "Origin": f"http://localhost:{PORT}"
}
print("[+] Isolated environment ready. Token acquired.")

# Force password change for admin
form_change_headers = {
    "Authorization": f"Bearer {admin_token}",
    "X-Lighthouse-Client": "web",
    "Origin": f"http://localhost:{PORT}"
}
res = requests.post(f"{BASE_URL}/user/change-password", headers=form_change_headers, data={"password": "admin123", "current_password": "admin"})
if res.status_code != 200:
    print(f"[-] FAILED: Could not change admin password: {res.text}")
    SERVER_PROC.terminate()
    sys.exit(1)
print("[+] Admin password changed to satisfy FORCE_PASSWORD_CHANGE.")

# Re-authenticate to get updated JWT claims (password_changed = true)
admin_token = get_token("admin", "admin123")
if not admin_token:
    print("[-] FAILED: Could not log in after password change")
    SERVER_PROC.terminate()
    sys.exit(1)

auth_headers["Authorization"] = f"Bearer {admin_token}"
form_change_headers["Authorization"] = f"Bearer {admin_token}"

print("\n--- Running Module Tests ---")

failures = []
def assert_status(res, expected, test_name):
    if res.status_code != expected:
        failures.append(f"{test_name} - Expected {expected}, got {res.status_code}. Response: {res.text}")
        print(f"  [X] {test_name} FAILED")
    else:
        print(f"  [+] {test_name} PASSED")

# --- Test 1: Auth & RBAC ---
print("\n[Testing Auth & RBAC]")
# Create a read-only user (Admin/Users uses JSON payload? No, user creation typically uses JSON, let's check)
# Let's assume JSON for now, if it fails we'll adjust.
ro_user_payload = {"username": "readonly", "password": "password", "role_template_id": 2, "is_admin": "false", "authMethod": "local"}
res = requests.post(f"{BASE_URL}/admin/users", headers=form_change_headers, data=ro_user_payload)
assert_status(res, 201, "Create Read-Only User")

ro_token = get_token("readonly", "password")
if not ro_token:
    failures.append("Failed to login as readonly user")
    print("  [X] Read-Only User Login FAILED")
else:
    print("  [+] Read-Only User Login PASSED")
    ro_headers = {
        "Authorization": f"Bearer {ro_token}",
        "Content-Type": "application/json",
        "X-Lighthouse-Client": "web",
        "Origin": f"http://localhost:{PORT}"
    }
    res = requests.post(f"{BASE_URL}/gitops/projects", headers=ro_headers, json={"name": "hacked", "repository_url": "foo", "branch": "main"})
    assert_status(res, 403, "Read-Only User Blocked from Creating GitOps Project")

# --- Test 2: GitOps Webhooks Edge Cases ---
print("\n[Testing GitOps Webhooks]")
res = requests.post(f"{BASE_URL}/gitops/projects", headers=auth_headers, json={
    "name": "test-project", "repository_url": "https://github.com/test/repo", "branch": "main"
})
assert_status(res, 200, "Create GitOps Project")

secret = "e2e_test_secret"
# Trigger Webhook
res = requests.post(f"http://localhost:{PORT}/webhook/gitops", json={"ref": "refs/heads/main", "repository": {"url": "https://github.com/foo/bar.git"}}, headers={"X-Gitlab-Token": secret})
assert_status(res, 200, "Valid GitOps Webhook Trigger")

# Trigger Webhook for ignored branch
res = requests.post(f"http://localhost:{PORT}/webhook/gitops", json={"ref": "refs/heads/dev", "repository": {"url": "https://github.com/foo/bar.git"}}, headers={"X-Gitlab-Token": secret})
assert_status(res, 200, "Webhook skips ignored branch safely")

# --- Test 3: Alert Rules Delivery Constraints ---
print("\n[Testing Alerts Delivery]")
res = requests.put(f"{BASE_URL}/admin/settings", headers=auth_headers, json={
    "metrics_retention_days": 30, "smtp_host": "test.mail.com", "smtp_port": 587, "smtp_user": "test", "alerts_email_address": "admin@test.com"
})
assert_status(res, 200, "Configure SMTP in settings")

# /admin/alerts/rules uses FormValue, so we must use data=
# Plus, it needs a token without content-type application/json if sending form-data.
form_headers = {
    "Authorization": f"Bearer {admin_token}",
    "X-Lighthouse-Client": "web",
    "Origin": f"http://localhost:{PORT}"
}
res = requests.post(f"{BASE_URL}/admin/alerts/rules", headers=form_headers, data={
    "name": "Test No Channel", "container_pattern": ".*", "event_types": "test_event",
    "enable_slack": "false", "enable_email": "false", "enable_generic_webhook": "false", "enabled": "true"
})
assert_status(res, 201, "Create Alert Rule without channels")

print("\n=============================================")
if len(failures) == 0:
    print("SUCCESS: All End-to-End Tests Passed! ✅")
else:
    print(f"FAILED: {len(failures)} Tests Failed ❌")
    for f in failures:
        print(f" - {f}")

SERVER_PROC.terminate()
if os.path.exists(TEST_DB):
    os.remove(TEST_DB)
if len(failures) > 0:
    sys.exit(1)
