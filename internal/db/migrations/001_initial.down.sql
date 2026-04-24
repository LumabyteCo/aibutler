-- Revert Schema version 1: Drop all tables in reverse dependency order

DROP TABLE IF EXISTS schema_migrations;
DROP TABLE IF EXISTS resource_access_log;
DROP TABLE IF EXISTS agents;
DROP TABLE IF EXISTS token_usage;
DROP TABLE IF EXISTS task_contexts;
DROP TABLE IF EXISTS user_meals;
DROP TABLE IF EXISTS user_maintenance;
DROP TABLE IF EXISTS user_media;
DROP TABLE IF EXISTS user_places;
DROP TABLE IF EXISTS user_journal;
DROP TABLE IF EXISTS user_recipes;
DROP TABLE IF EXISTS user_subscriptions;
DROP TABLE IF EXISTS user_habit_logs;
DROP TABLE IF EXISTS user_habits;
DROP TABLE IF EXISTS user_health;
DROP TABLE IF EXISTS user_budgets;
DROP TABLE IF EXISTS user_expenses;
DROP TABLE IF EXISTS user_contacts;
DROP TABLE IF EXISTS user_reminders;
DROP TABLE IF EXISTS user_tasks;
DROP TABLE IF EXISTS captured_thoughts;
DROP TABLE IF EXISTS key_facts;
DROP TABLE IF EXISTS sessions;
