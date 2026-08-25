import "./index.css";
import { Composition, Folder } from "remotion";
import { StateFabricHero } from "./Composition";
import { ArchitectureScene } from "./scenes/ArchitectureScene";
import { FreshCloneScene } from "./scenes/FreshCloneScene";
import { HostPushScene } from "./scenes/HostPushScene";
import { OpeningScene } from "./scenes/OpeningScene";
import { ProofScene } from "./scenes/ProofScene";
import { SandboxRoundTripScene } from "./scenes/SandboxRoundTripScene";

export const RemotionRoot: React.FC = () => {
  return (
    <>
      <Composition
        id="StateFabricHero"
        component={StateFabricHero}
        durationInFrames={1365}
        fps={30}
        width={1920}
        height={1080}
      />
      <Folder name="State-Fabric-Hero-Scenes">
        <Composition
          id="Opening"
          component={OpeningScene}
          durationInFrames={150}
          fps={30}
          width={1920}
          height={1080}
        />
        <Composition
          id="Architecture"
          component={ArchitectureScene}
          durationInFrames={210}
          fps={30}
          width={1920}
          height={1080}
        />
        <Composition
          id="HostPush"
          component={HostPushScene}
          durationInFrames={270}
          fps={30}
          width={1920}
          height={1080}
        />
        <Composition
          id="SandboxRoundTrip"
          component={SandboxRoundTripScene}
          durationInFrames={300}
          fps={30}
          width={1920}
          height={1080}
        />
        <Composition
          id="FreshClone"
          component={FreshCloneScene}
          durationInFrames={270}
          fps={30}
          width={1920}
          height={1080}
        />
        <Composition
          id="Proof"
          component={ProofScene}
          durationInFrames={240}
          fps={30}
          width={1920}
          height={1080}
        />
      </Folder>
    </>
  );
};
