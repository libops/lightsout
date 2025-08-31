# Lights Out

A lightweight Go service that combines ping health checks with power management for GCE instances. Acts as both a health endpoint and an activity monitor that can automatically shut down instances after periods of inactivity.

## Features

- **HTTP Health Endpoints**: `/ping` (monitored) and `/health` (healthcheck)
- **Activity Monitoring**: Tracks requests to `/ping` endpoint
- **Configurable Timeouts**: Environment-controlled inactivity periods
- **GCP Integration**: Automatic instance suspension via GCP API service

## Usage

### As Docker Container

```bash
docker run -p 8808:8808 -e INACTIVITY_TIMEOUT=90 lightswitch:latest
```

### Environment Variables

| Variable             | Default | Description                              |
| -------------------- | ------- | ---------------------------------------- |
| `PORT`               | `8808`  | HTTP server port                         |
| `INACTIVITY_TIMEOUT` | `90`    | Seconds of inactivity before shutdown    |
| `CHECK_INTERVAL`     | `30`    | Seconds between activity checks          |
| `LIBOPS_PROXY_URL`   | -       | GCP proxy URL for suspension             |
| `LIBOPS_KEEP_ONLINE` | -       | Set to "yes" to disable auto-shutdown    |
| `LOG_LEVEL`          | `INFO`  | Logging level (DEBUG, INFO, WARN, ERROR) |

### Endpoints

- `GET /ping` - Returns "pong", activity is logged and monitored
- `GET /health` - Returns "healthy", used for container healthchecks

## Integration

This service is designed to work in tandem with [ppb (Proxy Power Button)](https://github.com/libops/ppb) to create a complete on-demand infrastructure solution:

### How ppb and lightsout work together:

1. **ppb (Boot)**: Acts as an ingress proxy that automatically starts GCE instances when requests arrive
   - Receives incoming requests for dormant instances
   - Starts the GCE instance via GCP API
   - Proxies requests to the now-running backend
   
2. **lightsout (Shutdown)**: Monitors activity and automatically shuts down instances during idle periods
   - Tracks activity via `/ping` endpoint calls
   - Monitors for configurable inactivity timeouts
   - Automatically suspends the GCE instance to save costs

### Deployment Architecture:

```
Internet → ppb (ingress) → GCE Instance (running lightsout + your app)
```

- Deploy ppb as your public-facing service
- Deploy lightsout alongside your application on GCE instances
- Configure your application to periodically call `/ping` to signal activity
- ppb handles instance startup, lightsout handles instance shutdown

### Required IAM Permissions

The Google Service Account (GSA) used by this service requires the following IAM permissions:

- `compute.instances.suspend` - To suspend/stop the GCE instance
- `compute.instances.get` - To check the current status of the instance

These can be granted via the predefined `Compute Instance Admin (v1)` role, or by creating a custom role with only the specific permissions needed:

```bash
# Create custom role with minimal permissions
gcloud iam roles create lightsout.instanceManager \
    --project=YOUR_PROJECT_ID \
    --title="Lightsout Instance Manager" \
    --description="Minimal permissions for lightsout service" \
    --permissions=compute.instances.suspend,compute.instances.get

# Assign to service account
gcloud projects add-iam-policy-binding YOUR_PROJECT_ID \
    --member="serviceAccount:YOUR_SERVICE_ACCOUNT@YOUR_PROJECT_ID.iam.gserviceaccount.com" \
    --role="projects/YOUR_PROJECT_ID/roles/lightsout.instanceManager"
```
