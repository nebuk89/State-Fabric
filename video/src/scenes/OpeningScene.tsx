import { Easing, Interactive, interpolate, useCurrentFrame } from "remotion";
import { Background } from "../components/Background";

export const OpeningScene: React.FC = () => {
  const frame = useCurrentFrame();

  return (
    <Background>
      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          flexDirection: "column",
          justifyContent: "center",
          padding: "0 120px",
        }}
      >
        <Interactive.Div
          name="Opening eyebrow"
          style={{
            color: "#66E3FF",
            fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
            fontSize: 28,
            fontWeight: 850,
            letterSpacing: 7,
            textTransform: "uppercase",
            opacity: interpolate(frame, [0, 18], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            }),
          }}
        >
          State Fabric / real off-box run
        </Interactive.Div>
        <Interactive.Div
          name="Opening title"
          style={{
            color: "#F7F8FF",
            fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
            fontSize: 132,
            lineHeight: 0.94,
            fontWeight: 880,
            letterSpacing: -8,
            maxWidth: 1500,
            marginTop: 32,
            opacity: interpolate(frame, [8, 34], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            }),
            translate: interpolate(frame, [8, 34], ["0px 52px", "0px 0px"], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            }),
          }}
        >
          Git learned one new address.
        </Interactive.Div>
        <Interactive.Div
          name="Opening subtitle"
          style={{
            color: "#A8B0C7",
            fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
            fontSize: 43,
            lineHeight: 1.35,
            maxWidth: 1250,
            marginTop: 38,
            opacity: interpolate(frame, [30, 55], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            }),
          }}
        >
          Normal Git commands. Near-agent cache. Signed state across two
          machines.
        </Interactive.Div>
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 18,
            marginTop: 58,
            opacity: interpolate(frame, [56, 78], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            }),
          }}
        >
          <div
            style={{
              width: 14,
              height: 14,
              borderRadius: "50%",
              backgroundColor: "#78F0B0",
              boxShadow: "0 0 28px #78F0B0",
            }}
          />
          <div
            style={{
              color: "#78F0B0",
              fontFamily: "SFMono-Regular, Menlo, Consolas, monospace",
              fontSize: 26,
              fontWeight: 700,
            }}
          >
            CAPTURED FROM A LIVE HERO JOURNEY
          </div>
        </div>
      </div>
    </Background>
  );
};
