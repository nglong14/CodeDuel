    FROM gcc:14-bookworm@sha256:5e927c284bf55a7dc796262e311a0703344f62f41f5621eb56843111b1d37e15

    RUN groupadd --gid 10001 sandbox \
        && useradd --uid 10001 --gid 10001 --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin sandbox

    ENV LANG=C.UTF-8 \
        LC_ALL=C.UTF-8 \
        HOME=/tmp

    USER 10001:10001
    WORKDIR /workspace
