-- Revert migration 019: Advanced Security

DROP TABLE IF EXISTS security_events;
DROP TABLE IF EXISTS webauthn_credentials;
