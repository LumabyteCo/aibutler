-- Revert Migration 018: Enterprise

DROP INDEX IF EXISTS idx_compliance_audit_time;
DROP INDEX IF EXISTS idx_compliance_audit_user;
DROP TABLE IF EXISTS compliance_audit;
DROP TABLE IF EXISTS oidc_sessions;
DROP TABLE IF EXISTS rbac_permissions;
DROP INDEX IF EXISTS idx_rbac_users_role;
DROP TABLE IF EXISTS rbac_users;
