#!/bin/bash

# Provisions and starts grafana server in devenv
# Do not fire without a devenv environment

# Check if we're in devenv 

if [ $DEVENV_ROOT ]; then
  echo "In devenv environment"
else
  echo "Not in devenv environment"
  exit 1
fi

# Get the current root of grafana
GRAFANA_ROOT=$(which grafana)
# Remove /bin/grafana from there
GRAFANA_ROOT=${GRAFANA_ROOT%/bin/grafana}
SHARED_GRAFANA_ROOT=$GRAFANA_ROOT/share/grafana

mkdir -p ./grafana

# Copy shared directory contents straight into ./grafana
cp -r $SHARED_GRAFANA_ROOT/* ./grafana/

# change ownership of the grafana directory to the current user
chown -R $(whoami):$(whoami) ./grafana

# Make sure that directory is writable by the current user
chmod -R u+w ./grafana

# Create a new custom configuration file for grafana 
FILE_CONTENTS="[server]
http_addr = 127.0.0.1
http_port = 3000

[paths]
data = data
logs = data/log
plugins = data/plugins
provisioning = conf/provisioning

[analytics]
reporting_enabled = false"

echo "$FILE_CONTENTS" > ./grafana/conf/custom.ini

# Start grafana server with the custom configuration
grafana server \
  --homepath "$PWD/grafana" \
  --config "$PWD/grafana/conf/custom.ini"