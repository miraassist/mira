#!/bin/bash
set -e

MODEL=${SPEAKER_MODEL:-wespeaker_en_voxceleb_resnet34_LM.onnx}
MODEL_PATH=/data/models/$MODEL
mkdir -p /data/models
if [ ! -f "$MODEL_PATH" ]; then
    echo "Downloading speaker model $MODEL"
    curl -fsSL -o "$MODEL_PATH.part" \
        "https://github.com/k2-fsa/sherpa-onnx/releases/download/speaker-recongition-models/$MODEL"
    mv "$MODEL_PATH.part" "$MODEL_PATH"
fi

ARGS=(
    --uri "tcp://0.0.0.0:10310"
    --upstream "${SPEAKER_UPSTREAM:-tcp://host.docker.internal:10300}"
    --model "$MODEL_PATH"
    --store /data/speakers.json
    --web-port 10311
    --threshold "${SPEAKER_THRESHOLD:-0.5}"
)
if [ "${SPEAKER_DEBUG:-false}" = "true" ]; then
    ARGS+=(--debug)
fi

exec python3 /app/speaker_id_wrapper.py "${ARGS[@]}"
