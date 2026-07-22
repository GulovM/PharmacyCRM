CREATE TABLE pharmacycrm_schema_metadata (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    schema_version bigint NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO pharmacycrm_schema_metadata (singleton, schema_version)
VALUES (true, 1);
