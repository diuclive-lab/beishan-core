import Foundation
import AVFoundation
import Observation

@Observable
final class IOSAudioRecorder: NSObject {
    var isRecording = false
    var lastRecordedURL: URL?
    
    private var audioRecorder: AVAudioRecorder?
    
    func startRecording() -> Result<Void, IOSAudioRecorderError> {
        let permission = AVAudioApplication.shared.recordPermission
        switch permission {
        case .denied:
            isRecording = false
            return .failure(.microphonePermissionDenied)
        case .undetermined:
            isRecording = false
            return .failure(.microphonePermissionNotDetermined)
        case .granted:
            break
        @unknown default:
            isRecording = false
            return .failure(.microphonePermissionDenied)
        }

        let session = AVAudioSession.sharedInstance()
        do {
            try session.setCategory(.playAndRecord, mode: .default)
            try session.setActive(true)
            
            let tempDir = FileManager.default.temporaryDirectory
            let fileURL = tempDir.appendingPathComponent("recording-\(UUID().uuidString).m4a")
            
            let settings: [String: Any] = [
                AVFormatIDKey: Int(kAudioFormatMPEG4AAC),
                AVSampleRateKey: 44100,
                AVNumberOfChannelsKey: 1,
                AVEncoderAudioQualityKey: AVAudioQuality.high.rawValue
            ]
            
            audioRecorder = try AVAudioRecorder(url: fileURL, settings: settings)
            guard audioRecorder?.record() == true else {
                isRecording = false
                return .failure(.recordingDidNotStart)
            }
            isRecording = true
            lastRecordedURL = fileURL
            return .success(())
        } catch {
            isRecording = false
            return .failure(.startFailed(error.localizedDescription))
        }
    }
    
    func stopRecording() -> Result<URL, IOSAudioRecorderError> {
        guard isRecording else {
            return .failure(.notRecording)
        }
        audioRecorder?.stop()
        isRecording = false
        guard let url = lastRecordedURL else {
            return .failure(.missingRecordingFile)
        }
        guard
            let attributes = try? FileManager.default.attributesOfItem(atPath: url.path),
            let size = attributes[.size] as? NSNumber,
            size.intValue > 0
        else {
            return .failure(.emptyRecording)
        }
        return .success(url)
    }
}

enum IOSAudioRecorderError: LocalizedError {
    case microphonePermissionDenied
    case microphonePermissionNotDetermined
    case recordingDidNotStart
    case startFailed(String)
    case notRecording
    case missingRecordingFile
    case emptyRecording

    var errorDescription: String? {
        switch self {
        case .microphonePermissionDenied:
            return "麦克风权限未开启，请在系统设置中允许 BeiShan 使用麦克风。"
        case .microphonePermissionNotDetermined:
            return "需要先开启麦克风权限后才能录音。"
        case .recordingDidNotStart:
            return "录音启动失败，请稍后重试。"
        case .startFailed(let detail):
            return detail.isEmpty ? "录音启动失败。" : "录音启动失败：\(detail)"
        case .notRecording:
            return "当前没有正在进行的录音。"
        case .missingRecordingFile:
            return "没有找到录音文件。"
        case .emptyRecording:
            return "没有录到音频。"
        }
    }
}
