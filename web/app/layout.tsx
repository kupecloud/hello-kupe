import type { Metadata } from "next";
import { themeBootstrap } from "./theme";
import "./globals.css";

export const metadata: Metadata = {
  title: "Hello Kupe · Match a little. Discover a lot.",
  icons: { icon: "/kupe-mark.svg" },
  description:
    "A little memory game with a cloud full of possibilities. Match Kupe capabilities and leave your mark on the leaderboard.",
};
export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeBootstrap }} />
      </head>
      <body>{children}</body>
    </html>
  );
}
