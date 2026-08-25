import {
  AbsoluteFill,
  Easing,
  interpolate,
  useCurrentFrame,
  useVideoConfig,
} from "remotion";

export const Background: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const frame = useCurrentFrame();
  const { durationInFrames } = useVideoConfig();

  return (
    <AbsoluteFill style={{ backgroundColor: "#070A12", overflow: "hidden" }}>
      <AbsoluteFill
        style={{
          backgroundImage:
            "linear-gradient(rgba(102,227,255,0.07) 1px, transparent 1px), linear-gradient(90deg, rgba(102,227,255,0.07) 1px, transparent 1px)",
          backgroundSize: "72px 72px",
          opacity: 0.34,
          translate: interpolate(
            frame,
            [0, durationInFrames],
            ["0px 0px", "-72px -36px"],
            {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.linear,
            },
          ),
        }}
      />
      <div
        style={{
          position: "absolute",
          width: 900,
          height: 900,
          borderRadius: "50%",
          background:
            "radial-gradient(circle, rgba(78,102,255,0.24) 0%, rgba(78,102,255,0) 68%)",
          top: -420,
          right: -180,
          translate: interpolate(
            frame,
            [0, durationInFrames],
            ["0px 0px", "-120px 110px"],
            {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            },
          ),
        }}
      />
      <div
        style={{
          position: "absolute",
          width: 700,
          height: 700,
          borderRadius: "50%",
          background:
            "radial-gradient(circle, rgba(61,232,181,0.17) 0%, rgba(61,232,181,0) 70%)",
          left: -280,
          bottom: -350,
        }}
      />
      {children}
    </AbsoluteFill>
  );
};
