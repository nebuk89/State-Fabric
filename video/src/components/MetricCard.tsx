import { Easing, interpolate, useCurrentFrame } from "remotion";

export const MetricCard: React.FC<{
  value: string;
  label: string;
  detail?: string;
  accent: string;
  from: number;
}> = ({ value, label, detail, accent, from }) => {
  const frame = useCurrentFrame();

  return (
    <div
      style={{
        padding: "28px 30px",
        borderRadius: 22,
        border: `1px solid ${accent}44`,
        backgroundColor: "rgba(11,16,29,0.82)",
        opacity: interpolate(frame, [from, from + 14], [0, 1], {
          extrapolateLeft: "clamp",
          extrapolateRight: "clamp",
          easing: Easing.bezier(0.16, 1, 0.3, 1),
        }),
        translate: interpolate(
          frame,
          [from, from + 14],
          ["0px 22px", "0px 0px"],
          {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          },
        ),
      }}
    >
      <div
        style={{
          color: accent,
          fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
          fontSize: 54,
          fontWeight: 850,
          letterSpacing: -2,
        }}
      >
        {value}
      </div>
      <div
        style={{
          color: "#F7F8FF",
          fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
          fontSize: 24,
          fontWeight: 750,
          marginTop: 4,
        }}
      >
        {label}
      </div>
      {detail ? (
        <div
          style={{
            color: "#7D879F",
            fontFamily: "SFMono-Regular, Menlo, Consolas, monospace",
            fontSize: 17,
            marginTop: 9,
          }}
        >
          {detail}
        </div>
      ) : null}
    </div>
  );
};
