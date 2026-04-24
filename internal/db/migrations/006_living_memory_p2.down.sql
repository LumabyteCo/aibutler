-- Revert Migration 006: Living Memory Phase 2

DROP TRIGGER IF EXISTS session_transcripts_fts_update;
DROP TRIGGER IF EXISTS session_transcripts_fts_delete;
DROP TRIGGER IF EXISTS session_transcripts_fts_insert;
DROP TRIGGER IF EXISTS captured_thoughts_fts_update;
DROP TRIGGER IF EXISTS captured_thoughts_fts_delete;
DROP TRIGGER IF EXISTS captured_thoughts_fts_insert;

DROP TABLE IF EXISTS memory_vectors;
DROP TABLE IF EXISTS session_transcripts_fts;
DROP TABLE IF EXISTS captured_thoughts_fts;
DROP TABLE IF EXISTS session_transcripts;
DROP TABLE IF EXISTS entity_relationships;
DROP TABLE IF EXISTS entities;
