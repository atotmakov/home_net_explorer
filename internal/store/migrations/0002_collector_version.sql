-- Schema v2 (T099): the collector build that sent the latest run, shown on the Collectors page.
ALTER TABLE collectors ADD COLUMN last_version TEXT NOT NULL DEFAULT '';
