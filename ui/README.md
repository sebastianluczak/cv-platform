The UI posts uploads to `/api/upload`, listens for Server-Sent Events at
`/api/events?job_id=<job-id>`, and retrieves completed role match scores from
`/api/results?job_id=<job-id>`. The Vite development proxy forwards these routes
to the uploader on `http://localhost:8080`. Set `VITE_UPLOAD_ENDPOINT` and
`VITE_EVENTS_ENDPOINT` when the UI needs to use different API URLs.
