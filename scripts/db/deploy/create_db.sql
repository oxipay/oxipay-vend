-- Deploy vendproxy:create_db to mysql

BEGIN;

CREATE DATABASE IF NOT EXISTS vend;
-- you need to grant user access
COMMIT;
