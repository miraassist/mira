FROM python:3.12-slim

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir \
    wyoming==1.10.2 \
    numpy \
    aiohttp \
    sherpa-onnx

RUN mkdir -p /data/models

COPY speaker_id_wrapper.py speaker_id_handler.py speaker_store.py web.py entrypoint.sh /app/
RUN chmod +x /app/entrypoint.sh

# Wyoming STT proxy, and the enrollment page
EXPOSE 10310 10311

ENTRYPOINT ["/app/entrypoint.sh"]
