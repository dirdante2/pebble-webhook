# Connecting the Pebble app

The web app shows every value under *Status → Pebble-App* with show and copy buttons.

1. In the Pebble app open **Index 01 → Webhooks** and configure **both gestures** the same way.
2. **URL**: `https://<your-domain>/hook`
3. **Payload**: *Both* (audio and transcript).
4. **Headers**: name `X-Webhook-Token` (setting *Header für den Token*), value = webhook token. No ring header is needed: the app identifies the ring by the identifier inside the delivery ID (`ring_<12 hex>-…`); the ring ID is `#` plus its last three characters.
5. **Signed requests**: on, secret = signing secret.
6. **Test**: *Send test event* – a test entry appears in the history. Then record once per gesture: *hold* creates a note, *click + hold* a planning (configurable).

If nothing arrives, *Recent runs* in the Pebble app shows the server's answer (401 = header or signature wrong). Pebble retries failed deliveries; duplicates are ignored.
