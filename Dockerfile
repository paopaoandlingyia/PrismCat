FROM debian:bookworm-slim
WORKDIR /app
COPY prismcat config.yaml ./
RUN mkdir -p data
EXPOSE 8080
CMD ["./prismcat", "-config", "config.yaml"]