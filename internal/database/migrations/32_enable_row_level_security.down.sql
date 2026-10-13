DROP EVENT TRIGGER IF EXISTS enable_rls_on_new_tables;
DROP FUNCTION IF EXISTS enable_rls_on_new_tables();

DO $$
DECLARE t record;
BEGIN
    FOR t IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
        EXECUTE format('ALTER TABLE public.%I DISABLE ROW LEVEL SECURITY', t.tablename);
    END LOOP;
END $$;
