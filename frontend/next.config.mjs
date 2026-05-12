/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  async rewrites() {
    const backendUrl = process.env.API_PROXY_URL || "http://localhost:8080";
    return [
      {
        source: "/api/:path*",
        destination: `${backendUrl}/api/:path*`,
      },
      {
        source: "/portal/:path*",
        destination: `${backendUrl}/portal/:path*`,
      },
      {
        source: "/merchant/:path*",
        destination: `${backendUrl}/merchant/:path*`,
      },
    ]
  },
};

export default nextConfig;
