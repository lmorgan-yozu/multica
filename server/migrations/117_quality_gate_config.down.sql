DROP TABLE IF EXISTS issue_quality_gate_event;

ALTER TABLE project
    DROP COLUMN IF EXISTS quality_gate_config;
