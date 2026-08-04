FROM ubuntu:24.04

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends python3 ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /app/bin /app/workspace

COPY server.py /app/server.py

ENV HOME=/root \
    AGY_PATH=/app/bin/agy \
    AGY_WORKSPACE=/app/workspace \
    PORT=8080

EXPOSE 8080
CMD ["python3", "/app/server.py"]
