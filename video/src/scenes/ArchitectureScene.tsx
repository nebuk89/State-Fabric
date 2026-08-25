import { Easing, interpolate, useCurrentFrame } from "remotion";
import { Background } from "../components/Background";
import { SceneHeader } from "../components/SceneHeader";

const nodes = [
  { label: "Agent", detail: "git push", color: "#F7F8FF" },
  { label: "Local cache", detail: "git-remote-fabric", color: "#66E3FF" },
  { label: "Signed edge", detail: "receipt", color: "#A78BFA" },
  { label: "Authority", detail: "journal + refs", color: "#78F0B0" },
] as const;

export const ArchitectureScene: React.FC = () => {
  const frame = useCurrentFrame();

  return (
    <Background>
      <SceneHeader
        eyebrow="The contract"
        title="Agents keep speaking Git."
        subtitle="State Fabric moves the durable source, workspace, and provenance graphs behind Git's supported remote-helper boundary."
      />
      <div
        style={{
          position: "absolute",
          left: 96,
          right: 96,
          top: 465,
          display: "grid",
          gridTemplateColumns: "repeat(4, 1fr)",
          gap: 62,
          alignItems: "center",
        }}
      >
        {nodes.map((node, index) => (
          <div key={node.label} style={{ position: "relative" }}>
            <div
              style={{
                height: 230,
                borderRadius: 28,
                padding: "38px 34px",
                border: `1px solid ${node.color}66`,
                backgroundColor: "rgba(10,15,27,0.9)",
                boxShadow: `0 24px 80px ${node.color}18`,
                opacity: interpolate(
                  frame,
                  [32 + index * 18, 48 + index * 18],
                  [0, 1],
                  {
                    extrapolateLeft: "clamp",
                    extrapolateRight: "clamp",
                    easing: Easing.bezier(0.16, 1, 0.3, 1),
                  },
                ),
                translate: interpolate(
                  frame,
                  [32 + index * 18, 48 + index * 18],
                  ["0px 34px", "0px 0px"],
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
                  width: 52,
                  height: 52,
                  borderRadius: 16,
                  backgroundColor: `${node.color}1f`,
                  border: `1px solid ${node.color}77`,
                  boxShadow: `0 0 30px ${node.color}28`,
                }}
              />
              <div
                style={{
                  color: "#F7F8FF",
                  fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
                  fontSize: 34,
                  fontWeight: 820,
                  marginTop: 27,
                  letterSpacing: -1,
                }}
              >
                {node.label}
              </div>
              <div
                style={{
                  color: node.color,
                  fontFamily: "SFMono-Regular, Menlo, Consolas, monospace",
                  fontSize: 20,
                  marginTop: 10,
                }}
              >
                {node.detail}
              </div>
            </div>
            {index < nodes.length - 1 ? (
              <div
                style={{
                  position: "absolute",
                  width: 62,
                  height: 2,
                  left: "100%",
                  top: 115,
                  backgroundColor: "rgba(102,227,255,0.28)",
                  overflow: "hidden",
                }}
              >
                <div
                  style={{
                    width: 22,
                    height: 2,
                    backgroundColor: "#66E3FF",
                    boxShadow: "0 0 14px #66E3FF",
                    translate: interpolate(
                      frame,
                      [62 + index * 18, 102 + index * 18],
                      ["-24px 0px", "64px 0px"],
                      {
                        extrapolateLeft: "clamp",
                        extrapolateRight: "clamp",
                        easing: Easing.linear,
                      },
                    ),
                  }}
                />
              </div>
            ) : null}
          </div>
        ))}
      </div>
      <div
        style={{
          position: "absolute",
          bottom: 95,
          left: 96,
          right: 96,
          color: "#A8B0C7",
          fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
          fontSize: 34,
          textAlign: "center",
          opacity: interpolate(frame, [122, 150], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
        }}
      >
        No forked Git binary. No mandatory hooks. No new workflow for the agent.
      </div>
    </Background>
  );
};
