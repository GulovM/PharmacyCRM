-- E2-DB-001: catalog and import staging.
-- Verification query: SELECT to_regclass('public.products') IS NOT NULL AND to_regclass('public.import_rows') IS NOT NULL;
-- Lock/rewrite assessment: new baseline objects only; no existing-row rewrite.
-- Compatibility: additive baseline; application traffic starts after the complete baseline.
-- Forward-fix policy: destructive down migrations are prohibited.

-- Catalog and staging imports
-- =====================================================================

CREATE TABLE products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title varchar(255) NOT NULL CHECK (btrim(title) <> ''),
    inn varchar(255),
    dosage varchar(100),
    form varchar(100) NOT NULL CHECK (btrim(form) <> ''),
    manufacturer varchar(255) NOT NULL CHECK (btrim(manufacturer) <> ''),
    country varchar(100),
    is_prescription_required boolean NOT NULL DEFAULT false,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE', 'ARCHIVED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_products_search_title ON products (lower(title));
CREATE INDEX idx_products_search_inn ON products (lower(inn));

CREATE TABLE product_presentations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id uuid NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    package_name varchar(100) NOT NULL CHECK (btrim(package_name) <> ''),
    inner_unit_name varchar(100),
    base_units_per_package bigint NOT NULL CHECK (base_units_per_package > 0),
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE', 'ARCHIVED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_presentation_inner_unit CHECK (
        base_units_per_package = 1
        OR (inner_unit_name IS NOT NULL AND btrim(inner_unit_name) <> '')
    )
);

CREATE TABLE product_barcodes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_presentation_id uuid NOT NULL
        REFERENCES product_presentations(id) ON DELETE RESTRICT,
    barcode varchar(64) NOT NULL CHECK (btrim(barcode) <> ''),
    is_primary boolean NOT NULL DEFAULT false,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_product_barcodes_value_active
ON product_barcodes (barcode)
WHERE status = 'ACTIVE';

CREATE UNIQUE INDEX uq_product_barcodes_primary
ON product_barcodes (product_presentation_id)
WHERE is_primary = true AND status = 'ACTIVE';

CREATE TABLE product_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    requested_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    raw_name varchar(255) NOT NULL CHECK (btrim(raw_name) <> ''),
    raw_details jsonb NOT NULL DEFAULT '{}'::jsonb,
    status varchar(30) NOT NULL DEFAULT 'OPEN'
        CHECK (status IN ('OPEN', 'APPROVED', 'REJECTED', 'DUPLICATE')),
    resolved_product_presentation_id uuid
        REFERENCES product_presentations(id) ON DELETE RESTRICT,
    resolved_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    resolution_note text,
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    CONSTRAINT chk_product_request_resolution CHECK (
        (status = 'OPEN'
            AND resolved_product_presentation_id IS NULL
            AND resolved_by_user_id IS NULL
            AND resolved_at IS NULL
            AND resolution_note IS NULL)
        OR (status <> 'OPEN'
            AND resolved_by_user_id IS NOT NULL
            AND resolved_at IS NOT NULL
            AND btrim(resolution_note) <> '')
    ),
    CONSTRAINT chk_product_request_target CHECK (
        status NOT IN ('APPROVED', 'DUPLICATE')
        OR resolved_product_presentation_id IS NOT NULL
    )
);

CREATE TABLE import_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    import_type varchar(50) NOT NULL
        CHECK (import_type IN ('CATALOG', 'INITIAL_STOCK')),
    pharmacy_id uuid REFERENCES pharmacies(id) ON DELETE RESTRICT,
    created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    original_filename varchar(255) NOT NULL CHECK (btrim(original_filename) <> ''),
    content_type varchar(150) NOT NULL CHECK (btrim(content_type) <> ''),
    storage_key text NOT NULL CHECK (btrim(storage_key) <> ''),
    file_sha256 bytea NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'UPLOADED'
        CHECK (status IN (
            'UPLOADED', 'VALIDATING', 'READY', 'HAS_ERRORS',
            'CONFIRMING', 'COMPLETED', 'FAILED'
        )),
    total_rows bigint NOT NULL DEFAULT 0 CHECK (total_rows >= 0),
    valid_rows bigint NOT NULL DEFAULT 0 CHECK (valid_rows >= 0),
    error_rows bigint NOT NULL DEFAULT 0 CHECK (error_rows >= 0),
    result_resource_type varchar(100),
    result_resource_id uuid,
    failure_code varchar(100),
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    CONSTRAINT chk_import_scope CHECK (
        (import_type = 'CATALOG' AND pharmacy_id IS NULL)
        OR (import_type = 'INITIAL_STOCK' AND pharmacy_id IS NOT NULL)
    ),
    CONSTRAINT chk_import_counts CHECK (valid_rows + error_rows <= total_rows),
    CONSTRAINT chk_import_terminal CHECK (
        (status IN ('COMPLETED', 'FAILED') AND completed_at IS NOT NULL)
        OR (status NOT IN ('COMPLETED', 'FAILED') AND completed_at IS NULL)
    ),
    CONSTRAINT chk_import_failure CHECK (
        (status = 'FAILED' AND failure_code IS NOT NULL)
        OR (status <> 'FAILED' AND failure_code IS NULL)
    )
);

CREATE TABLE import_rows (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    import_job_id uuid NOT NULL REFERENCES import_jobs(id) ON DELETE RESTRICT,
    row_number bigint NOT NULL CHECK (row_number > 0),
    raw_data jsonb NOT NULL,
    normalized_data jsonb,
    status varchar(30) NOT NULL
        CHECK (status IN (
            'PENDING', 'VALID', 'ERROR', 'MATCHED',
            'CREATE_NEW', 'REJECTED', 'PUBLISHED'
        )),
    matched_product_presentation_id uuid
        REFERENCES product_presentations(id) ON DELETE RESTRICT,
    validation_errors jsonb NOT NULL DEFAULT '[]'::jsonb,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_import_row_number UNIQUE (import_job_id, row_number),
    CONSTRAINT chk_import_row_match CHECK (
        status <> 'MATCHED' OR matched_product_presentation_id IS NOT NULL
    )
);

CREATE INDEX idx_import_rows_status
ON import_rows (import_job_id, status, row_number);

-- =====================================================================
