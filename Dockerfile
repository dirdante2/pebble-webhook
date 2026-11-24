# Docker-Image der Web-App: dieselben Binaries wie die Release-Dateien (vom Workflow in out/ gebaut).
# Lokal: Binary nach out/pebble-webhook-linux-<arch> bauen, dann docker buildx build --platform linux/<arch> .
# Kein Updater im Container: die App zeigt neue Versionen nur an, aktualisiert wird per neuem Image.

FROM busybox:1.37 AS ordner
RUN mkdir -p /config/data/users /config/keys /config/medien /config/backup \
 && chmod 0750 /config /config/data /config/data/users /config/medien /config/backup \
 && chmod 0700 /config/keys \
 && chown -R 65532:65532 /config

FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETARCH
ARG IMAGE=""
COPY --chmod=0755 out/pebble-webhook-linux-${TARGETARCH} /usr/local/bin/pebble-webhook
COPY --from=ordner --chown=65532:65532 /config /config
# Pfade und API-Schlüssel (z. B. OPENAI_API_KEY) lassen sich per Umgebung setzen; alles Weitere liegt in /config.
ENV PEBBLE_DOCKER=1 \
    PEBBLE_IMAGE=${IMAGE} \
    PEBBLE_ENV_FILE=/config/env \
    LISTEN=0.0.0.0:8080 \
    DATA_DIR=/config/data \
    KEY_DIR=/config/keys \
    MEDIA_DIR=/config/medien \
    BACKUP_DIR=/config/backup
VOLUME ["/config"]
EXPOSE 8080
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s CMD ["/usr/local/bin/pebble-webhook", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/pebble-webhook"]
CMD ["serve"]
