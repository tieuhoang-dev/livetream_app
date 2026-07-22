package services

import (
	"crypto/rand"
	"errors"
	"fmt"
	"livesteam_app/config"
	"livesteam_app/models"
	"livesteam_app/utils"
	"math/big"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func generateOTP() (string, error) {
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n), nil
}
func RegisterUser(username, email, password string) error {
	var existingUser models.User
	if err := config.DB.Where("email = ?", email).First(&existingUser).Error; err == nil {
		return gorm.ErrRegistered
	} else if err != gorm.ErrRecordNotFound {
		return err
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	otp, err := generateOTP()
	if err != nil {
		return err
	}
	otpHash, err := bcrypt.GenerateFromPassword([]byte(otp), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	otpExpiresAt := time.Now().Add(time.Minute * 15)
	user := models.User{
		ID:                    uuid.New().String(),
		Username:              username,
		Email:                 email,
		PasswordHash:          string(hashedPassword),
		IsVerified:            false,
		VerificationCodeHash:  string(otpHash),
		VerificationExpiresAt: &otpExpiresAt,
		Status:                "pending",
		Role:                  "user",
	}
	if err := config.DB.Create(&user).Error; err != nil {
		return err
	}
	go func() {
		utils.SendVerificationEmail(user.Email, otp)
	}()
	return nil
}
func VerifyUser(email, code string) error {
	var user models.User
	if err := config.DB.Where("email = ?", email).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return errors.New("user_not_found")
		}
		return err
	}

	if user.VerificationExpiresAt != nil && time.Now().After(*user.VerificationExpiresAt) {
		return errors.New("otp_expired")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.VerificationCodeHash), []byte(code)); err != nil {
		return errors.New("invalid_otp")
	}

	user.IsVerified = true
	user.Status = "active"
	user.VerificationCodeHash = ""
	user.VerificationExpiresAt = nil

	if err := config.DB.Save(&user).Error; err != nil {
		return errors.New("failed_to_update_user")
	}

	return nil
}
func Login(email, password string) (string, error) {
	var user models.User
	if err := config.DB.Where("email = ?", email).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", errors.New("user_not_found")
		}
		return "", errors.New("database_error")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", errors.New("invalid_password")
	}
	if !user.IsVerified {
		return "", errors.New("user_not_verified")
	}
	token, err := utils.GenerateJWT(user.ID, user.Role, user.Username)
	if err != nil {
		return "", errors.New("failed_to_generate_token")
	}
	return token, nil
}
func SetupProfile(ID string, fullname string, avatar string) error {
	var user models.User
	err := config.DB.Where("id = ?", ID).First(&user).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return errors.New("user_not_found")
		}
		return errors.New("database_error")
	}
	user.Fullname = fullname
	user.Avatar = avatar
	if err := config.DB.Save(&user).Error; err != nil {
		return errors.New("failed_to_update_profile")
	}
	return nil
}
func ResetPasswordRequest(email string) error {
	var user models.User
	err := config.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		if err == gorm.ErrCheckConstraintViolated {
			return errors.New("user_not_found")
		}
		return errors.New("database_error")
	}
	otp, err := generateOTP()
	if err != nil {
		return errors.New("failed_to_generate_otp")
	}
	otpHash, err := bcrypt.GenerateFromPassword([]byte(otp), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	otpExpiresAt := time.Now().Add(time.Minute * 15)
	user.VerificationCodeHash = string(otpHash)
	user.VerificationExpiresAt = &otpExpiresAt
	if err := config.DB.Save(&user).Error; err != nil {
		return errors.New("failed_to_update_user")
	}
	go func() {
		utils.SendVerificationEmail(email, otp)
	}()
	return nil
}
func ResetPassword(email, code, newPassword string) error {
	var user models.User
	if err := config.DB.Where("email = ?", email).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return errors.New("user_not_found")
		}
		return errors.New("database_error")
	}
	if user.IsVerified {
		return errors.New("user_already_verified")
	}
	if user.VerificationExpiresAt != nil && time.Now().After(*user.VerificationExpiresAt) {
		return errors.New("otp_expired")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.VerificationCodeHash), []byte(code)); err != nil {
		return errors.New("invalid_otp")
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hashedPassword)
	user.VerificationCodeHash = ""
	user.VerificationExpiresAt = nil
	if err := config.DB.Save(&user).Error; err != nil {
		return errors.New("failed_to_update_user")
	}
	return nil
}
func UpdateUserAvatar(userID string, avatarURL string) error {
	err := config.DB.Model(&models.User{}).
		Where("id = ?", userID).
		Update("avatar", avatarURL).Error
	return err
}
