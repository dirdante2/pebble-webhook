# Connecting the Pebble app

The web app shows every value under *Status → Pebble app*, with show and copy buttons.

## Steps

1. In the Pebble app open **Index 01 → Webhooks**.
2. Configure **both gestures** the same way.
3. **URL**: `https://<your-domain>/api/v1/pebble/hook`
4. **Payload**: *Both* (audio and transcript).
5. **Headers**: name `X-Webhook-Token` (setting *Header for the token*), value = webhook token.
6. **Signed requests**: on, secret = signing secret.
7. **Test**: *Send test event*. A test entry appears in the history.
8. Record once per gesture:
   - *hold* creates a note
   - *click + hold* creates a planning (configurable)

## Ring header

- No ring header is needed.
- The app identifies the ring by the identifier inside the delivery ID (`ring_<12 hex>-…`).
- The ring ID is `#` plus its last three characters.

## Troubleshooting

- If nothing arrives, check *Recent runs* in the Pebble app. It shows the server's answer.
- 401 = header or signature wrong.
- Pebble retries failed deliveries. Duplicates are ignored.
