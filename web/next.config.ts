import type { NextConfig } from "next";

const development = process.env.NODE_ENV === "development";
const config: NextConfig = {
  ...(development
    ? {
        rewrites: async () => [
          {
            source: "/api/:path*",
            destination: `${process.env.KUPE_API_URL || "http://localhost:8080"}/api/:path*`,
          },
        ],
      }
    : { output: "export" }),
  poweredByHeader: false,
  skipTrailingSlashRedirect: true,
  trailingSlash: true,
};
export default config;
