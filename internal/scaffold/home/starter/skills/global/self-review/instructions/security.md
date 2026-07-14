---
scope: "Security checklist for all changes touching input, auth, data, or config"
---

# Security Checklist

## Secrets and Credentials

- [ ] No API keys, tokens, passwords, or secrets hardcoded in source.
- [ ] No secrets in comments, log statements, or error messages.
- [ ] `.env` files and credential stores are in `.gitignore`.
- [ ] If secrets are needed, they come from environment variables or a secret manager.

## Input Validation

- [ ] All user input is validated before use (type, length, format, range).
- [ ] File paths from user input are sanitized against path traversal (`../`).
- [ ] URL parameters and query strings are validated and escaped.
- [ ] JSON/XML parsing has size limits and schema validation where appropriate.

## Injection Prevention

- [ ] SQL queries use parameterized statements, never string concatenation.
- [ ] HTML output is escaped to prevent XSS.
- [ ] Shell commands do not interpolate user input. Use argument arrays.
- [ ] Template rendering escapes variables by default.

## Authentication and Authorization

- [ ] Auth checks are not bypassed by the new code paths.
- [ ] Privilege escalation is not possible through the changes.
- [ ] Session tokens are not logged or exposed in URLs.
- [ ] Rate limiting exists on endpoints that accept user input.

## Data Handling

- [ ] Sensitive data (PII, financial) is not logged.
- [ ] Data at rest and in transit uses appropriate encryption.
- [ ] Temporary files with sensitive content are cleaned up.
- [ ] CORS, CSP, and other security headers are not weakened.

## Dependencies

- [ ] New dependencies are from trusted sources with active maintenance.
- [ ] No known vulnerabilities in added or updated packages.
- [ ] Dependency versions are pinned or locked.
