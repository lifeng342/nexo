#!/bin/bash
# Wait for MySQL and Redis to be ready

set -e

MAX_RETRIES=30
RETRY_INTERVAL=2

echo "Waiting for MySQL..."
for i in $(seq 1 $MAX_RETRIES); do
    if docker exec nexo_mysql mysqladmin ping -h localhost --silent 2>/dev/null; then
        echo "MySQL is ready!"
        break
    fi
    if [ $i -eq $MAX_RETRIES ]; then
        echo "MySQL failed to start after $MAX_RETRIES attempts"
        exit 1
    fi
    echo "Waiting for MySQL... ($i/$MAX_RETRIES)"
    sleep $RETRY_INTERVAL
done

echo "Waiting for Redis..."
for i in $(seq 1 $MAX_RETRIES); do
    if docker exec nexo_redis redis-cli ping 2>/dev/null | grep -q PONG; then
        echo "Redis is ready!"
        break
    fi
    if [ $i -eq $MAX_RETRIES ]; then
        echo "Redis failed to start after $MAX_RETRIES attempts"
        exit 1
    fi
    echo "Waiting for Redis... ($i/$MAX_RETRIES)"
    sleep $RETRY_INTERVAL
done

echo "All services are ready!"
