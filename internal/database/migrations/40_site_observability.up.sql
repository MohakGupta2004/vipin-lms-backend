-- One row per browser that accepted cookies (visitor_id lives in the vg_vid cookie).
CREATE TABLE site_visitors (
    visitor_id uuid PRIMARY KEY,
    fingerprint text NOT NULL DEFAULT '',
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    visit_count int NOT NULL DEFAULT 1,
    last_session_id text NOT NULL DEFAULT '',
    pageviews int NOT NULL DEFAULT 0,
    device_type text NOT NULL DEFAULT '',
    browser text NOT NULL DEFAULT '',
    os text NOT NULL DEFAULT '',
    language text NOT NULL DEFAULT '',
    timezone text NOT NULL DEFAULT '',
    screen text NOT NULL DEFAULT '',
    country text NOT NULL DEFAULT '',
    region text NOT NULL DEFAULT '',
    city text NOT NULL DEFAULT '',
    first_referrer text NOT NULL DEFAULT '',
    first_landing text NOT NULL DEFAULT '',
    utm_source text NOT NULL DEFAULT '',
    utm_medium text NOT NULL DEFAULT '',
    utm_campaign text NOT NULL DEFAULT ''
);
CREATE INDEX site_visitors_first_seen_idx ON site_visitors (first_seen_at);
CREATE INDEX site_visitors_last_seen_idx ON site_visitors (last_seen_at);
CREATE INDEX site_visitors_fingerprint_idx ON site_visitors (fingerprint);

-- Every tracked action. visitor_id is NULL for people who declined cookies (anonymous pageviews only).
CREATE TABLE site_events (
    id bigserial PRIMARY KEY,
    visitor_id uuid REFERENCES site_visitors(visitor_id) ON DELETE CASCADE,
    session_id text NOT NULL DEFAULT '',
    event_type text NOT NULL CHECK (event_type IN ('pageview', 'click', 'form_submit')),
    path text NOT NULL DEFAULT '',
    label text NOT NULL DEFAULT '',
    target text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX site_events_created_idx ON site_events (created_at);
CREATE INDEX site_events_visitor_idx ON site_events (visitor_id, created_at);

-- Every website form submission. One table, kind says which form.
CREATE TABLE site_leads (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind text NOT NULL CHECK (kind IN ('masterclass', 'diagnostic', 'contact', 'firms', 'newsletter', 'interest')),
    name text NOT NULL DEFAULT '',
    email text NOT NULL,
    phone text NOT NULL DEFAULT '',
    course_slugs text[] NOT NULL DEFAULT '{}',
    company_name text NOT NULL DEFAULT '',
    team_size text NOT NULL DEFAULT '',
    exam_date text NOT NULL DEFAULT '',
    message text NOT NULL DEFAULT '',
    casl_consent boolean NOT NULL DEFAULT false,
    source_path text NOT NULL DEFAULT '',
    visitor_id uuid,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX site_leads_kind_created_idx ON site_leads (kind, created_at DESC);
CREATE INDEX site_leads_course_slugs_idx ON site_leads USING gin (course_slugs);
