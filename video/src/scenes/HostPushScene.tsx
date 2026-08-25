import { Easing, interpolate, useCurrentFrame } from "remotion";
import { Background } from "../components/Background";
import { MetricCard } from "../components/MetricCard";
import { SceneHeader } from "../components/SceneHeader";
import { TerminalWindow } from "../components/TerminalWindow";
import { heroRun } from "../hero-data";

export const HostPushScene: React.FC = () => {
  const frame = useCurrentFrame();

  return (
    <Background>
      <SceneHeader
        eyebrow="Machine one / this host"
        title="Push with the Git agents already know."
        subtitle="The local cache snapshots complete history and finalizes through the off-box authority."
      />
      <div
        style={{
          position: "absolute",
          left: 96,
          top: 380,
          width: 1160,
          height: 570,
          opacity: interpolate(frame, [24, 44], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
        }}
      >
        <TerminalWindow
          title="State Fabric hero repository"
          badge="this host"
          accent="#66E3FF"
          lines={[
            { text: "git init -b main", from: 42, kind: "command" },
            {
              text: "git remote add origin fabric://local-cache/hero",
              from: 70,
              kind: "command",
            },
            { text: "git push -u origin main", from: 105, kind: "command" },
            { text: "[new branch]  main -> main", from: 142, kind: "output" },
            { text: "authority finalized", from: 170, kind: "success" },
            {
              text: `commit ${heroRun.hostCommit.slice(0, 12)}`,
              from: 194,
              kind: "muted",
            },
          ]}
        />
      </div>
      <div
        style={{
          position: "absolute",
          left: 1304,
          right: 96,
          top: 432,
          display: "grid",
          gap: 18,
        }}
      >
        <MetricCard
          value={String(heroRun.hostStats.objects)}
          label="private objects"
          detail={`node ${heroRun.nodes.host}`}
          accent="#66E3FF"
          from={118}
        />
        <MetricCard
          value={String(heroRun.hostStats.transitions)}
          label="signed transition"
          accent="#A78BFA"
          from={148}
        />
        <MetricCard
          value={String(heroRun.hostStats.receipts)}
          label="durability receipt"
          accent="#78F0B0"
          from={178}
        />
      </div>
    </Background>
  );
};
