import { Easing, Interactive, interpolate, useCurrentFrame } from "remotion";
import { Background } from "../components/Background";
import { MetricCard } from "../components/MetricCard";
import { heroRun } from "../hero-data";

export const ProofScene: React.FC = () => {
  const frame = useCurrentFrame();

  return (
    <Background>
      <div
        style={{
          position: "absolute",
          inset: "90px 96px",
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
        }}
      >
        <Interactive.Div
          name="Proof eyebrow"
          style={{
            color: "#78F0B0",
            fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
            fontSize: 25,
            fontWeight: 850,
            letterSpacing: 6,
            textTransform: "uppercase",
            opacity: interpolate(frame, [0, 18], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            }),
          }}
        >
          Verified on both machines
        </Interactive.Div>
        <Interactive.Div
          name="Proof title"
          style={{
            color: "#F7F8FF",
            fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
            fontSize: 96,
            lineHeight: 1,
            fontWeight: 880,
            letterSpacing: -5,
            textAlign: "center",
            maxWidth: 1500,
            marginTop: 24,
            opacity: interpolate(frame, [8, 30], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            }),
          }}
        >
          One Git workflow. Two machines. Durable state.
        </Interactive.Div>
        <div
          style={{
            width: "100%",
            display: "grid",
            gridTemplateColumns: "repeat(3, 1fr)",
            gap: 26,
            marginTop: 72,
          }}
        >
          <MetricCard
            value={String(heroRun.freshCacheStats.objects)}
            label="objects"
            detail="authority = fresh cache"
            accent="#66E3FF"
            from={46}
          />
          <MetricCard
            value={String(heroRun.freshCacheStats.transitions)}
            label="transitions"
            detail="signed and replayable"
            accent="#A78BFA"
            from={62}
          />
          <MetricCard
            value={String(heroRun.freshCacheStats.receipts)}
            label="receipts"
            detail="host-disk durability"
            accent="#78F0B0"
            from={78}
          />
        </div>
        <Interactive.Div
          name="Proof statement"
          style={{
            color: "#DCE1F2",
            fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
            fontSize: 45,
            lineHeight: 1.35,
            textAlign: "center",
            maxWidth: 1420,
            marginTop: 62,
            opacity: interpolate(frame, [106, 132], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            }),
          }}
        >
          Normal Git UX. Near-agent state. Signed authority.
        </Interactive.Div>
        <div
          style={{
            marginTop: "auto",
            color: "#707991",
            fontFamily: "SFMono-Regular, Menlo, Consolas, monospace",
            fontSize: 21,
            opacity: interpolate(frame, [142, 166], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
              easing: Easing.bezier(0.16, 1, 0.3, 1),
            }),
          }}
        >
          LIVE RUN {heroRun.runDate} / {heroRun.hostCommit.slice(0, 8)} -&gt;{" "}
          {heroRun.remoteCommit.slice(0, 8)}
        </div>
      </div>
    </Background>
  );
};
