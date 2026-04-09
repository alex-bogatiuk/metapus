/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${process.env.API_PROXY_URL || "http://localhost:8080"}/api/:path*`,
      },
    ]
  },
};

export default nextConfig;
