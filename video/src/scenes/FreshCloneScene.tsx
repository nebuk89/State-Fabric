import { Easing, interpolate, useCurrentFrame } from "remotion";
import { Background } from "../components/Background";
import { SceneHeader } from "../components/SceneHeader";
import { TerminalWindow } from "../components/TerminalWindow";
import { heroRun } from "../hero-data";

export const FreshCloneScene: React.FC = () => {
  const frame = useCurrentFrame();

  return (
    <Background>
      <SceneHeader
        eyebrow="Back on this host / empty cache"
        title="The returned state arrives nearby."
        subtitle="A brand-new cache hydrates the verified journal suffix and complete Git bundle from the off-box authority."
      />
      <div
        style={{
          position: "absolute",
          left: 96,
          top: 375,
          right: 96,
          height: 575,
          opacity: interpolate(frame, [22, 44], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
        }}
      >
        <TerminalWindow
          title="Fresh local cache"
          badge={`node ${heroRun.nodes.freshCache}`}
          accent="#78F0B0"
          lines={[
            {
              text: "git clone fabric://fresh-cache/hero",
              from: 36,
              kind: "command",
            },
            { text: `HEAD ${heroRun.remoteCommit}`, from: 76, kind: "success" },
            { text: "git fabric status", from: 112, kind: "command" },
            {
              text: `Authority ref refs/heads/main at ${heroRun.remoteCommit.slice(0, 12)}`,
              from: 145,
              kind: "output",
            },
            {
              text: `Transition ${heroRun.transitionId.slice(0, 30)}...`,
              from: 174,
              kind: "muted",
            },
            {
              text: `mission.md  sha256:${heroRun.missionSha256.slice(0, 16)}...  OK`,
              from: 204,
              kind: "success",
            },
            {
              text: `sandbox.md sha256:${heroRun.sandboxSha256.slice(0, 16)}...  OK`,
              from: 230,
              kind: "success",
            },
          ]}
        />
      </div>
    </Background>
  );
};
