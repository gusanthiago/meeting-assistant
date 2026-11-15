FROM alpine:3.24.4
RUN apk add --no-cache pgbouncer && (id pgbouncer >/dev/null 2>&1 || adduser -D -H pgbouncer) && pgbouncer --version
COPY entrypoint.sh /entrypoint.sh
USER pgbouncer
EXPOSE 6432
ENTRYPOINT ["/bin/sh", "/entrypoint.sh"]
