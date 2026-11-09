import path from "node:path";
import { fileURLToPath } from "node:url";
import createBundleAnalyzer from "@next/bundle-analyzer";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const withBundleAnalyzer = createBundleAnalyzer({
  enabled: process.env.ANALYZE === "true",
});

/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  turbopack: {
    root: __dirname,
  },
  async rewrites() {
    const backendUrl = process.env.API_PROXY_URL || "http://localhost:8080";
    return [
      {
        source: "/api/:path*",
        destination: `${backendUrl}/api/:path*`,
      },
      {
        source: "/portal/v1/:path*",
        destination: `${backendUrl}/portal/v1/:path*`,
      },
      {
        source: "/merchant/:path*",
        destination: `${backendUrl}/merchant/:path*`,
      },
    ]
  },
};

export default withBundleAnalyzer(nextConfig);
