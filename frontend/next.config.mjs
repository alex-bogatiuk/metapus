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
  // Companions of the Content-Security-Policy set per page in proxy.ts.
  // HSTS belongs to the TLS terminator (nginx) and is set there.
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "Referrer-Policy", value: "same-origin" },
          { key: "Cross-Origin-Opener-Policy", value: "same-origin-allow-popups" },
          { key: "Cross-Origin-Resource-Policy", value: "same-origin" },
          { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=(), payment=(), usb=(), browsing-topics=()" },
        ],
      },
    ]
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
