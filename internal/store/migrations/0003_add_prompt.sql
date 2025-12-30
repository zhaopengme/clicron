-- Add prompt field to tasks table
-- This field stores the original prompt that gets converted to a full command
ALTER TABLE tasks ADD COLUMN prompt TEXT;
