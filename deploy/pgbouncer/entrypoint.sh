#!/bin/sh
set -eu
dir=/tmp/pgbouncer
mkdir -p "$dir"
cat > "$dir/pgbouncer.ini" <<INI
[databases]
${POSTGRES_DB} = host=${PGBOUNCER_DB_HOST:-postgres} port=5432 dbname=${POSTGRES_DB}

[pgbouncer]
listen_addr = 0.0.0.0
listen_port = 6432
unix_socket_dir =
auth_type = scram-sha-256
auth_file = $dir/userlist.txt
pool_mode = ${PGBOUNCER_POOL_MODE:-transaction}
default_pool_size = ${PGBOUNCER_DEFAULT_POOL_SIZE:-20}
max_client_conn = ${PGBOUNCER_MAX_CLIENT_CONN:-500}
max_prepared_statements = ${PGBOUNCER_MAX_PREPARED_STATEMENTS:-200}
ignore_startup_parameters = extra_float_digits
INI
printf '"%s" "%s"\n' "$POSTGRES_USER" "$POSTGRES_PASSWORD" > "$dir/userlist.txt"
exec pgbouncer "$dir/pgbouncer.ini"
