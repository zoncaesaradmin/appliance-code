CREATE TABLE ordering_merchants (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE ordering_offerings (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    merchant_id text NOT NULL REFERENCES ordering_merchants(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    price_minor bigint NOT NULL CHECK (price_minor >= 0),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE ordering_merchant_memberships (
    merchant_id text NOT NULL REFERENCES ordering_merchants(id) ON DELETE CASCADE,
    user_id text NOT NULL CHECK (user_id ~ '^[0-9a-f]{32}$'),
    role text NOT NULL CHECK (role IN ('manager','operator')),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (merchant_id, user_id)
);

CREATE TABLE ordering_offering_micro_zones (
    offering_id text NOT NULL REFERENCES ordering_offerings(id) ON DELETE CASCADE,
    micro_zone_id text NOT NULL CHECK (micro_zone_id ~ '^[A-Za-z0-9_-]{2,128}$'),
    PRIMARY KEY (offering_id, micro_zone_id)
);

CREATE TABLE ordering_orders (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    user_id text NOT NULL CHECK (user_id ~ '^[0-9a-f]{32}$'),
    merchant_id text NOT NULL REFERENCES ordering_merchants(id),
    micro_zone_id text NOT NULL CHECK (micro_zone_id ~ '^[A-Za-z0-9_-]{2,128}$'),
    channel_id text NOT NULL CHECK (channel_id ~ '^[A-Za-z0-9._:-]{2,128}$'),
    conversation_id text CHECK (conversation_id IS NULL OR conversation_id ~ '^[A-Za-z0-9._:-]{2,256}$'),
    correlation_id text NOT NULL CHECK (correlation_id ~ '^[A-Za-z0-9._:-]{2,128}$'),
    status text NOT NULL CHECK (status IN ('submitted','acknowledged','accepted','declined','fulfilled','cancelled')),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    total_minor bigint NOT NULL CHECK (total_minor >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE ordering_order_lines (
    order_id text NOT NULL REFERENCES ordering_orders(id) ON DELETE CASCADE,
    line_number integer NOT NULL CHECK (line_number > 0),
    offering_id text NOT NULL REFERENCES ordering_offerings(id),
    offering_name text NOT NULL,
    unit_price_minor bigint NOT NULL CHECK (unit_price_minor >= 0),
    quantity integer NOT NULL CHECK (quantity BETWEEN 1 AND 1000),
    line_total_minor bigint NOT NULL CHECK (line_total_minor >= 0),
    PRIMARY KEY (order_id, line_number)
);

CREATE TABLE ordering_idempotency_keys (
    user_id text NOT NULL CHECK (user_id ~ '^[0-9a-f]{32}$'),
    request_key text NOT NULL CHECK (request_key ~ '^[A-Za-z0-9._:-]{8,128}$'),
    request_hash bytea NOT NULL,
    order_id text REFERENCES ordering_orders(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, request_key)
);

CREATE INDEX ordering_offerings_zone_idx
    ON ordering_offering_micro_zones (micro_zone_id, offering_id);
CREATE INDEX ordering_orders_user_idx
    ON ordering_orders (user_id, created_at DESC);

GRANT USAGE ON SCHEMA public TO ordering_runtime;
GRANT SELECT, INSERT, UPDATE ON ordering_merchants, ordering_merchant_memberships, ordering_offerings,
    ordering_offering_micro_zones, ordering_orders, ordering_order_lines,
    ordering_idempotency_keys TO ordering_runtime;

