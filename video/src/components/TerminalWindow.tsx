import { Easing, interpolate, useCurrentFrame } from "remotion";

export type TerminalLine = {
  text: string;
  from: number;
  kind?: "command" | "output" | "success" | "muted";
};

export const TerminalWindow: React.FC<{
  title: string;
  badge: string;
  accent: string;
  lines: readonly TerminalLine[];
}> = ({ title, badge, accent, lines }) => {
  const frame = useCurrentFrame();

  return (
    <div
      style={{
        width: "100%",
        height: "100%",
        borderRadius: 24,
        overflow: "hidden",
        backgroundColor: "rgba(10,14,25,0.94)",
        border: `1px solid ${accent}55`,
        boxShadow: `0 28px 90px ${accent}1f`,
      }}
    >
      <div
        style={{
          height: 68,
          display: "flex",
          alignItems: "center",
          padding: "0 24px",
          borderBottom: "1px solid rgba(255,255,255,0.08)",
          backgroundColor: "rgba(255,255,255,0.025)",
        }}
      >
        <div style={{ display: "flex", gap: 10 }}>
          {["#FF6B7C", "#F8C35C", "#4FD59A"].map((color) => (
            <div
              key={color}
              style={{
                width: 13,
                height: 13,
                borderRadius: "50%",
                backgroundColor: color,
              }}
            />
          ))}
        </div>
        <div
          style={{
            marginLeft: 24,
            color: "#DCE1F2",
            fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
            fontSize: 23,
            fontWeight: 700,
          }}
        >
          {title}
        </div>
        <div
          style={{
            marginLeft: "auto",
            color: accent,
            fontFamily: "SFMono-Regular, Menlo, Consolas, monospace",
            fontSize: 18,
            fontWeight: 800,
            letterSpacing: 2,
            textTransform: "uppercase",
            padding: "8px 14px",
            borderRadius: 999,
            border: `1px solid ${accent}66`,
            backgroundColor: `${accent}14`,
          }}
        >
          {badge}
        </div>
      </div>
      <div style={{ padding: "28px 34px" }}>
        {lines.map((line) => {
          const color =
            line.kind === "success"
              ? "#78F0B0"
              : line.kind === "muted"
                ? "#707991"
                : line.kind === "command"
                  ? "#F7F8FF"
                  : "#A8B0C7";
          return (
            <div
              key={`${line.from}-${line.text}`}
              style={{
                minHeight: 47,
                color,
                fontFamily: "SFMono-Regular, Menlo, Consolas, monospace",
                fontSize: 28,
                lineHeight: 1.45,
                fontWeight: line.kind === "command" ? 700 : 500,
                whiteSpace: "pre-wrap",
                opacity: interpolate(
                  frame,
                  [line.from, line.from + 9],
                  [0, 1],
                  {
                    extrapolateLeft: "clamp",
                    extrapolateRight: "clamp",
                    easing: Easing.bezier(0.16, 1, 0.3, 1),
                  },
                ),
                translate: interpolate(
                  frame,
                  [line.from, line.from + 9],
                  ["0px 12px", "0px 0px"],
                  {
                    extrapolateLeft: "clamp",
                    extrapolateRight: "clamp",
                    easing: Easing.bezier(0.16, 1, 0.3, 1),
                  },
                ),
              }}
            >
              {line.kind === "command" ? (
                <span style={{ color: accent }}>$ </span>
              ) : null}
              {line.text}
            </div>
          );
        })}
      </div>
    </div>
  );
};
