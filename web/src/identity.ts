export type IdentityBar = { level: number; tone: number };

export function identityBars(value: string | undefined, count: number): IdentityBar[] {
  const digest = normalizeDigest(value);
  return Array.from({ length: count }, (_, index) => {
    const byte = Number.parseInt(digest.slice(index * 2, index * 2 + 2), 16);
    return { level: 2 + Math.floor((byte / 255) * 7), tone: byte % 4 };
  });
}

export function abbreviateIdentity(value?: string): string {
  const digest = normalizeDigest(value);
  return value ? `${digest.slice(0, 10)} / ${digest.slice(-10)}` : "identity pending";
}

export function normalizeDigest(value?: string): string {
  const digest = (value || "").replace(/^sha256:/, "").replace(/[^0-9a-f]/gi, "").toLowerCase();
  return (digest || "0").padEnd(64, digest || "0").slice(0, 64);
}
