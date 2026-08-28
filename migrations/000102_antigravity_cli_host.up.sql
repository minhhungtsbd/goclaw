-- Move the retired Docker sidecar provider to the host-native AGY bridge.
UPDATE llm_providers
SET provider_type = 'antigravity_cli_host',
    api_base = CASE
        WHEN api_base = 'http://antigravity-runtime:8080/v1' THEN ''
        ELSE api_base
    END
WHERE provider_type = 'antigravity_cli';
