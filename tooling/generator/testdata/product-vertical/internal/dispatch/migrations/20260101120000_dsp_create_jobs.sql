-- +goose Up
CREATE TABLE dsp_jobs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reference   text NOT NULL
                CHECK (char_length(reference) BETWEEN 1 AND 64),
    status      text NOT NULL DEFAULT 'queued'
                CHECK (status IN ('queued', 'running', 'done')),
    created_by  uuid NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dsp_jobs_reference_key UNIQUE (reference)
);
