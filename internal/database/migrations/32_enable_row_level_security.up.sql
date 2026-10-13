-- Row level security on every table, so Supabase's public API (anon / authenticated roles, no policies)
-- can't read or write anything. The backend connects as the table owner, which bypasses RLS.
DO $$
DECLARE t record;
BEGIN
    FOR t IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
        EXECUTE format('ALTER TABLE public.%I ENABLE ROW LEVEL SECURITY', t.tablename);
    END LOOP;
END $$;

-- Tables created by later migrations get RLS too, without anyone having to remember it.
CREATE OR REPLACE FUNCTION enable_rls_on_new_tables() RETURNS event_trigger
LANGUAGE plpgsql AS $$
DECLARE obj record;
BEGIN
    FOR obj IN SELECT * FROM pg_event_trigger_ddl_commands()
        WHERE command_tag IN ('CREATE TABLE', 'CREATE TABLE AS', 'SELECT INTO') AND schema_name = 'public'
    LOOP
        EXECUTE format('ALTER TABLE %s ENABLE ROW LEVEL SECURITY', obj.object_identity);
    END LOOP;
END $$;

CREATE EVENT TRIGGER enable_rls_on_new_tables ON ddl_command_end
    WHEN TAG IN ('CREATE TABLE', 'CREATE TABLE AS', 'SELECT INTO')
    EXECUTE FUNCTION enable_rls_on_new_tables();
