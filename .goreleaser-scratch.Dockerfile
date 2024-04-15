FROM gcr.io/distroless/static-debian12:latest
WORKDIR /app
COPY snooper-* /app/snooper
RUN ln -s /app/snooper /app/json_rpc_snoop
EXPOSE 3000
CMD ["./snooper", "--help"]
