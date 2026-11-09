-- Read-only schema inventory. Run only against the disposable acceptance DB.
-- No tenant rows, secrets or function bodies are exported.
COPY (
 SELECT n.nspname AS schema_name,p.proname AS function_name,
  pg_get_function_identity_arguments(p.oid) AS arguments,p.prokind AS routine_kind,
  md5(pg_get_functiondef(p.oid)) AS definition_md5,
  (p.prosrc ~* '(insert[[:space:]]+into|update[[:space:]]+|delete[[:space:]]+from|execute[[:space:]])') AS sql_write,
  (p.prosrc ILIKE '%ON CONFLICT%') AS on_conflict,
  COALESCE((SELECT string_agg(c.relname||'.'||t.tgname||'[enabled='||t.tgenabled::text||';definition='||md5(pg_get_triggerdef(t.oid))||']',';' ORDER BY c.relname,t.tgname)
   FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE t.tgfoid=p.oid AND NOT t.tgisinternal),'') AS trigger_bindings
 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='public' AND p.prokind IN ('f','p')
 ORDER BY p.proname,pg_get_function_identity_arguments(p.oid)
) TO STDOUT WITH CSV HEADER;
