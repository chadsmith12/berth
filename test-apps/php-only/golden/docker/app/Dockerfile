# =============================================================================
# Berth generated Laravel image — Inertia + PostgreSQL
# PHP 8.5 / Node 22 / 
# =============================================================================

ARG PHP_VERSION=8.5
ARG NODE_VERSION=22

# -----------------------------------------------------------------------------
# base
# -----------------------------------------------------------------------------
FROM dunglas/frankenphp:1-php${PHP_VERSION} AS base

RUN install-php-extensions \
        pcntl \
        pdo_pgsql \
        zip \
        intl \
        gd \
        bcmath \
        opcache

WORKDIR /app

# node — a named stage, because BuildKit rejects variable expansion in
# COPY --from. Stages build lazily, so PHP-only projects never pull it.
FROM node:${NODE_VERSION}-bookworm-slim AS node

# -----------------------------------------------------------------------------
# vendor — caches on composer.lock alone
# -----------------------------------------------------------------------------
FROM base AS vendor

COPY --from=composer:2 /usr/bin/composer /usr/bin/composer
COPY composer.json composer.lock ./

RUN composer install \
        --no-dev \
        --no-scripts \
        --no-autoloader \
        --prefer-dist \
        --no-interaction

# -----------------------------------------------------------------------------
# assets — vendor + optimized autoload, plus the client bundle when the project has a JS toolchain
# -----------------------------------------------------------------------------
FROM vendor AS assets

ENV APP_ENV=production \
    APP_DEBUG=false \
    APP_KEY=base64:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=

COPY . .

RUN mkdir -p bootstrap/cache \
    && composer dump-autoload --optimize --classmap-authoritative --no-interaction \
    && php artisan package:discover --ansi

# -----------------------------------------------------------------------------
# runtime
# -----------------------------------------------------------------------------
FROM base AS runtime

COPY . .

COPY --from=assets /app/vendor          ./vendor
COPY --from=assets /app/bootstrap/cache ./bootstrap/cache

RUN mkdir -p storage/framework/cache storage/framework/sessions storage/framework/views storage/logs storage/app bootstrap/cache \
    && chown -R www-data:www-data storage bootstrap/cache \
    && chmod -R 775 storage bootstrap/cache

COPY docker/app/entrypoint.sh /usr/local/bin/entrypoint
RUN chmod +x /usr/local/bin/entrypoint

RUN { \
      echo 'opcache.enable=1'; \
      echo 'opcache.validate_timestamps=0'; \
      echo 'opcache.memory_consumption=192'; \
      echo 'opcache.max_accelerated_files=20000'; \
    } > /usr/local/etc/php/conf.d/opcache.ini

ENTRYPOINT ["entrypoint"]

CMD ["frankenphp", "run", "--config", "/etc/caddy/Caddyfile"]
