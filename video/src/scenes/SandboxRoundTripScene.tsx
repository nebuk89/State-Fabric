import { Easing, interpolate, useCurrentFrame } from "remotion";
import { Background } from "../components/Background";
import { MetricCard } from "../components/MetricCard";
import { SceneHeader } from "../components/SceneHeader";
import { TerminalWindow } from "../components/TerminalWindow";
import { heroRun } from "../hero-data";

export const SandboxRoundTripScene: React.FC = () => {
  const frame = useCurrentFrame();

  return (
    <Background>
      <SceneHeader
        eyebrow="Machine two / off-box Linux sandbox"
        title="Clone it there. Change it there. Push it back."
        subtitle="The remote machine sees an ordinary Git remote backed by its local authority state."
      />
      <div
        style={{
          position: "absolute",
          left: 96,
          top: 365,
          width: 1220,
          height: 610,
          opacity: interpolate(frame, [22, 44], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
            easing: Easing.bezier(0.16, 1, 0.3, 1),
          }),
        }}
      >
        <TerminalWindow
          title="Off-box authority"
          badge="linux sandbox"
          accent="#A78BFA"
          lines={[
            {
              text: "git clone fabric://authority/hero",
              from: 38,
              kind: "command",
            },
            {
              text: `HEAD ${heroRun.hostCommit.slice(0, 12)}`,
              from: 78,
              kind: "output",
            },
            {
              text: 'printf "remote proof" > sandbox.md',
              from: 112,
              kind: "command",
            },
            {
              text: 'git commit -am "return state from sandbox"',
              from: 148,
              kind: "command",
            },
            { text: "git push origin main", from: 184, kind: "command" },
            {
              text: `${heroRun.hostCommit.slice(0, 7)}..${heroRun.remoteCommit.slice(0, 7)}  main -> main`,
              from: 220,
              kind: "success",
            },
          ]}
        />
      </div>
      <div
        style={{
          position: "absolute",
          left: 1360,
          right: 96,
          top: 432,
          display: "grid",
          gap: 18,
        }}
      >
        <MetricCard
          value={String(heroRun.authorityStats.objects)}
          label="objects audited"
          detail={`authority ${heroRun.nodes.authority}`}
          accent="#66E3FF"
          from={118}
        />
        <MetricCard
          value={String(heroRun.authorityStats.transitions)}
          label="transitions"
          accent="#A78BFA"
          from={160}
        />
        <MetricCard
          value={String(heroRun.authorityStats.receipts)}
          label="receipts"
          detail="0 divergent refs"
          accent="#78F0B0"
          from={202}
        />
      </div>
    </Background>
  );
};
