import { TransitionSeries, linearTiming } from "@remotion/transitions";
import { fade } from "@remotion/transitions/fade";
import { ArchitectureScene } from "./scenes/ArchitectureScene";
import { FreshCloneScene } from "./scenes/FreshCloneScene";
import { HostPushScene } from "./scenes/HostPushScene";
import { OpeningScene } from "./scenes/OpeningScene";
import { ProofScene } from "./scenes/ProofScene";
import { SandboxRoundTripScene } from "./scenes/SandboxRoundTripScene";

export const StateFabricHero: React.FC = () => {
  return (
    <TransitionSeries>
      <TransitionSeries.Sequence durationInFrames={150} name="Opening">
        <OpeningScene />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition
        presentation={fade()}
        timing={linearTiming({ durationInFrames: 15 })}
      />
      <TransitionSeries.Sequence durationInFrames={210} name="Architecture">
        <ArchitectureScene />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition
        presentation={fade()}
        timing={linearTiming({ durationInFrames: 15 })}
      />
      <TransitionSeries.Sequence durationInFrames={270} name="Host push">
        <HostPushScene />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition
        presentation={fade()}
        timing={linearTiming({ durationInFrames: 15 })}
      />
      <TransitionSeries.Sequence
        durationInFrames={300}
        name="Sandbox round trip"
      >
        <SandboxRoundTripScene />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition
        presentation={fade()}
        timing={linearTiming({ durationInFrames: 15 })}
      />
      <TransitionSeries.Sequence
        durationInFrames={270}
        name="Fresh cache clone"
      >
        <FreshCloneScene />
      </TransitionSeries.Sequence>
      <TransitionSeries.Transition
        presentation={fade()}
        timing={linearTiming({ durationInFrames: 15 })}
      />
      <TransitionSeries.Sequence durationInFrames={240} name="Proof">
        <ProofScene />
      </TransitionSeries.Sequence>
    </TransitionSeries>
  );
};
