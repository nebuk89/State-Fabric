import { Easing, Interactive, interpolate, useCurrentFrame } from "remotion";

export const SceneHeader: React.FC<{
  eyebrow: string;
  title: string;
  subtitle: string;
}> = ({ eyebrow, title, subtitle }) => {
  const frame = useCurrentFrame();

  return (
    <div style={{ position: "absolute", left: 96, top: 76, right: 96 }}>
      <Interactive.Div
        name="Scene eyebrow"
        style={{
          color: "#66E3FF",
          fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
          fontSize: 24,
          fontWeight: 800,
          letterSpacing: 5,
          textTransform: "uppercase",
          opacity: interpolate(frame, [0, 12], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
        }}
      >
        {eyebrow}
      </Interactive.Div>
      <Interactive.Div
        name="Scene title"
        style={{
          color: "#F7F8FF",
          fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
          fontSize: 72,
          lineHeight: 1.02,
          fontWeight: 820,
          letterSpacing: -3,
          marginTop: 16,
          opacity: interpolate(frame, [5, 22], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
          translate: interpolate(frame, [5, 22], ["0px 26px", "0px 0px"], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
        }}
      >
        {title}
      </Interactive.Div>
      <Interactive.Div
        name="Scene subtitle"
        style={{
          color: "#A8B0C7",
          fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
          fontSize: 31,
          lineHeight: 1.35,
          marginTop: 18,
          maxWidth: 1100,
          opacity: interpolate(frame, [16, 34], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
        }}
      >
        {subtitle}
      </Interactive.Div>
    </div>
  );
};
