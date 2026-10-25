"""Wyoming STT proxy that recognizes the speaker.

Home Assistant talks to it as to any Wyoming STT. The audio goes on to the real STT (--upstream) and the
transcript comes back unchanged; beside it, a speaker embedding is compared with the enrolled speakers
and the result is logged and shown on the enrollment page (--web-port).
"""

import argparse
import asyncio
import logging
from functools import partial

from aiohttp import web
from wyoming.client import AsyncClient
from wyoming.info import AsrModel, AsrProgram, Attribution, Describe, Info
from wyoming.server import AsyncServer

from speaker_id_handler import SpeakerIdEventHandler
from speaker_store import SpeakerStore
from web import make_app

_LOGGER = logging.getLogger(__name__)

__version__ = "0.1.0"


async def upstream_info(uri: str) -> Info:
    """The upstream STT's description, retried until it answers."""
    while True:
        try:
            async with AsyncClient.from_uri(uri) as client:
                await client.write_event(Describe().event())
                while True:
                    event = await client.read_event()
                    if event is None:
                        raise ConnectionError("closed")
                    if Info.is_type(event.type):
                        return Info.from_event(event)
        except Exception as err:  # noqa: BLE001
            _LOGGER.warning("Upstream %s not ready (%s), retrying", uri, err)
            await asyncio.sleep(5)


async def main() -> None:
    parser = argparse.ArgumentParser(description="Speaker ID Wyoming STT proxy")
    parser.add_argument("--uri", required=True)
    parser.add_argument("--upstream", required=True, help="the real STT, e.g. tcp://host:10300")
    parser.add_argument("--model", required=True, help="sherpa-onnx speaker embedding model")
    parser.add_argument("--store", required=True, help="JSON file with the enrolled speakers")
    parser.add_argument("--web-port", type=int, default=10311)
    parser.add_argument("--threshold", type=float, default=0.7)
    parser.add_argument("--token", default="", help="the API's bearer token; empty: open")
    parser.add_argument("--debug", action="store_true")
    args = parser.parse_args()

    logging.basicConfig(
        level=logging.DEBUG if args.debug else logging.INFO,
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    )

    store = SpeakerStore(model_path=args.model, store_path=args.store, threshold=args.threshold)

    up = await upstream_info(args.upstream)
    models = [m for asr in up.asr for m in asr.models]
    upstream_name = up.asr[0].name if up.asr else "stt"
    _LOGGER.info("Upstream %s: %s", args.upstream, [m.name for m in models])

    wyoming_info = Info(
        asr=[
            AsrProgram(
                name="speaker-id",
                description=f"Speaker ID in front of {upstream_name}",
                attribution=Attribution(name="speaker-id", url="https://github.com/k2-fsa/sherpa-onnx"),
                installed=True,
                version=__version__,
                models=[
                    AsrModel(
                        name=f"speaker-id/{m.name}",
                        description=m.description,
                        attribution=m.attribution,
                        installed=True,
                        languages=m.languages,
                        version=m.version,
                    )
                    for m in models
                ],
                supports_transcript_streaming=False,
            )
        ],
    )

    runner = web.AppRunner(make_app(store, args.token))
    await runner.setup()
    await web.TCPSite(runner, "0.0.0.0", args.web_port).start()
    _LOGGER.info("API on port %d%s", args.web_port, "" if args.token else ", without a token")

    server = AsyncServer.from_uri(args.uri)
    _LOGGER.info("Wyoming proxy on %s", args.uri)
    await server.run(
        partial(SpeakerIdEventHandler, wyoming_info=wyoming_info, upstream=args.upstream, store=store)
    )


if __name__ == "__main__":
    asyncio.run(main())
