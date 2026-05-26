global:
  scrape_interval: 5s

scrape_configs:
  - job_name: "services"
    static_configs:
      - targets: ["__PROMETHEUS_SCRAPE_TARGET__"]
