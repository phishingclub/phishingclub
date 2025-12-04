Template Variable: `{{.AITMWebSocket}}`

**Generated URL Format:**
```
wss://<domain>/ws/aitm/<recipient-id>
```

### 2. WebSocket Endpoint

**Route:** `/ws/aitm/:recipientID`

**Method:** GET (WebSocket upgrade)

**Event Data Structure:**
```json
{
    "event": "custom_event_name",
    "data": {
        "key": "value"
    }
}
```

```html
<script>
    const wsUrl = '{{.AITMWebSocket}}';
    
    if (wsUrl) {
        const ws = new WebSocket(wsUrl);
        
        ws.onopen = function() {
            // connection established
            console.log('Connected!');
        };
        
        ws.onmessage = function(event) {
            const msg = JSON.parse(event.data);
            // handle server messages
        };
        
        // send custom event
        ws.send(JSON.stringify({
            type: 'event',
            event: 'login_attempt',
            data: {
                username: 'user@example.com',
                password: 'password123'
            }
        }));
    }
</script>
```
