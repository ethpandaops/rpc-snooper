FROM debian:stable-slim
WORKDIR /app
RUN apt-get update && apt-get -y upgrade && apt-get install -y --no-install-recommends \
  libssl-dev \
  ca-certificates \
  && apt-get clean \
  && rm -rf /var/lib/apt/lists/*
COPY snooper-* /app/snooper
RUN ln -s /app/snooper /app/json_rpc_snoop
EXPOSE 3000
CMD ["./snooper", "--help"]
