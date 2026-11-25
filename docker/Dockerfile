# Docker-Image der Web-App: dieselben Binaries wie die Release-Dateien (vom Workflow in out/ gebaut).
# Lokal (aus dem Repo-Wurzelordner): Binary nach out/pebble-webhook-linux-<arch> bauen, dann
#   docker buildx build -f docker/Dockerfile --platform linux/<arch> .
# Kein Updater im Container: die App zeigt neue Versionen nur an, aktualisiert wird per neuem Image.

FROM busybox:1.37@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec AS ordner
ARG IMAGE=""
# Markierungsdatei: schaltet den Docker-Modus fest ein (keine Umgebungsvariable) und nennt den Image-Namen.
RUN mkdir -p /config/data/users /config/keys /config/medien /config/backup /marker/etc \
 && chmod 0750 /config /config/data /config/data/users /config/medien /config/backup \
 && chmod 0700 /config/keys \
 && chown -R 1000:1000 /config \
 && echo "${IMAGE}" > /marker/etc/pebble-webhook-docker

FROM gcr.io/distroless/static-debian12@sha256:d75cdd72874d4790092fcb1b058493ecf6bb5bf2b2b897045b00ff01d91843f2
ARG TARGETARCH
COPY --chmod=0755 out/pebble-webhook-linux-${TARGETARCH} /usr/local/bin/pebble-webhook
COPY --from=ordner /config /config
COPY --from=ordner /marker/etc/pebble-webhook-docker /etc/pebble-webhook-docker
# Startet als root, richtet die Ordner auf PUID:PGID (Vorgabe 1000:1000) und läuft dann als dieser Nutzer.
# Pfade und API-Schlüssel (z. B. OPENAI_API_KEY) lassen sich per Umgebung setzen; alles Weitere liegt in /config.
ENV PUID=1000 \
    PGID=1000 \
    PEBBLE_ENV_FILE=/config/env \
    LISTEN=0.0.0.0:8080 \
    DATA_DIR=/config/data \
    KEY_DIR=/config/keys \
    MEDIA_DIR=/config/medien \
    BACKUP_DIR=/config/backup
VOLUME ["/config"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s CMD ["/usr/local/bin/pebble-webhook", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/pebble-webhook"]
CMD ["serve"]
