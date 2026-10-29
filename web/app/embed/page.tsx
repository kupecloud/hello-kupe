import type { Metadata } from "next";
import Game from "../Game";

export const metadata: Metadata = {
  title: "Hello Kupe · Memory game",
  robots: { index: false, follow: false },
};

export default function Embed() {
  return <Game embedded />;
}
