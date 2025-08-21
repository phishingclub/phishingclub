#!/bin/sh
# Get the IP address of the backend container
BACKEND_IP=$(getent hosts backend | awk '{ print $1 }')

# Update the dnsmasq configuration file
echo "address=/.test/$BACKEND_IP" > /etc/dnsmasq.conf
echo "BACKEND_IP=$BACKEND_IP"
echo "Loaded configuration (/etc/dnsmasq.conf):"
cat /etc/dnsmasq.conf
# Run dnsmasq in the foreground
dnsmasq -k --port=5353
